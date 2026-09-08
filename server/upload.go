package chserver

import (
	"encoding/json"
	"fmt"
	"mime/multipart"
	"net/http"
	"path"
	"path/filepath"
	"strings"
	"sync"
	"time"

	errors2 "github.com/proximile/proxiport/server/api/errors"
	errors3 "github.com/proximile/proxiport/share/errors"

	"github.com/proximile/proxiport/server/api"
	"github.com/proximile/proxiport/server/auditlog"
	"github.com/proximile/proxiport/server/clients"
	"github.com/proximile/proxiport/server/clients/clientdata"
	"github.com/proximile/proxiport/share/comm"
	"github.com/proximile/proxiport/share/files"
	"github.com/proximile/proxiport/share/models"
	"github.com/proximile/proxiport/share/random"

	"github.com/google/uuid"
	"github.com/gorilla/websocket"
	"github.com/pkg/errors"
)

const uploadBufSize = 1000000 // 1Mb

type UploadRequest struct {
	File                 multipart.File
	FileHeader           *multipart.FileHeader
	ClientIDs            []string
	GroupIDs             []string
	ClientTags           *models.JobClientTags
	clientsInGroupsCount int
	Clients              []*clientdata.Client
	*models.UploadedFile
}

func (ur UploadRequest) GetClientIDs() (ids []string) {
	return ur.ClientIDs
}

func (ur UploadRequest) GetGroupIDs() (ids []string) {
	return ur.GroupIDs
}

func (ur UploadRequest) GetClientTags() (clientTags *models.JobClientTags) {
	return ur.ClientTags
}

func (al *APIListener) handleFileUploads(w http.ResponseWriter, req *http.Request) {
	uploadRequest, err := al.uploadRequestFromRequest(req)
	if err != nil {
		al.jsonError(w, err)
		return
	}

	curUser, err := al.getUserModelForAuth(req.Context())
	if err != nil {
		al.jsonErrorResponseWithTitle(w, http.StatusBadRequest, err.Error())
		return
	}
	defer func() {
		if cerr := uploadRequest.File.Close(); cerr != nil {
			al.Errorf("error closing the uploaded file: %v", cerr)
		}
	}()

	// The staging dir holds cleartext upload payloads until every target agent
	// has pulled them, so keep it owner-only (0700): no other host user should
	// be able to read a file in transit. Mounting GetUploadDir() on a tmpfs
	// keeps the payload off persistent disk entirely.
	wasCreated, err := al.filesAPI.CreateDirIfNotExists(al.config.GetUploadDir(), files.UploadStagingDirMode)
	if err != nil {
		al.jsonError(w, err)
		return
	}
	if wasCreated {
		al.Infof("created directory %s", al.config.GetUploadDir())
	}

	// The file id is used to build the server-side temp path; reject any value with
	// path separators so it cannot be used for path traversal / arbitrary file write.
	if strings.ContainsAny(uploadRequest.ID, `/\`) {
		al.jsonErrorResponseWithTitle(w, http.StatusBadRequest, fmt.Sprintf("invalid file id %q: must not contain path separators", uploadRequest.ID))
		return
	}

	uploadRequest.SourceFilePath = al.genFilePath(uploadRequest.ID)

	err = uploadRequest.Validate()
	if err != nil {
		al.jsonErrorResponseWithTitle(w, http.StatusBadRequest, err.Error())
		return
	}

	if err = validateRemoteDestination(uploadRequest); err != nil {
		al.jsonErrorResponseWithDetail(w, http.StatusBadRequest, "BAD_DESTINATION", "upload denied", err.Error())
		return
	}

	cr := clients.ClientServiceProvider{}
	clientGroups, err := al.clientGroupProvider.GetAll(req.Context())
	if err != nil {
		al.jsonError(w, err)
	}
	if err := cr.CheckClientsAccess(uploadRequest.Clients, curUser, clientGroups); err != nil {
		al.jsonErrorResponseWithDetail(w, http.StatusForbidden, "ACCESS_CONTROL_VIOLATION", "upload forbidden", err.Error())
		return
	}

	copiedBytes, err := al.filesAPI.CreateFile(uploadRequest.SourceFilePath, uploadRequest.File)
	if err != nil {
		al.jsonError(w, err)
		return
	}
	// Restrict the staged cleartext payload to the daemon only (0600); the
	// default create mode is group/other-readable.
	if err := al.filesAPI.ChangeMode(uploadRequest.SourceFilePath, files.UploadStagingFileMode); err != nil {
		al.Errorf("failed to restrict permissions on staged upload %s: %v", uploadRequest.SourceFilePath, err)
	}

	file, err := al.filesAPI.Open(uploadRequest.SourceFilePath)
	if err != nil {
		al.jsonError(w, err)
		return
	}

	md5Checksum, err := files.Md5HashFromReader(file)
	if err != nil {
		al.jsonError(w, err)
		return
	}

	uploadRequest.Md5Checksum = md5Checksum

	al.Debugf(
		"staged upload %s on server, size %d, Content-Type %s",
		uploadRequest.FileHeader.Filename,
		uploadRequest.FileHeader.Size,
		uploadRequest.FileHeader.Header.Get("Content-Type"),
	)

	uploadRep := &models.UploadResponseShort{
		ID:        uploadRequest.ID,
		Filepath:  uploadRequest.DestinationPath,
		SizeBytes: copiedBytes,
	}
	al.auditLog.Entry(auditlog.ApplicationUploads, auditlog.ActionCreate).
		WithHTTPRequest(req).
		WithRequest(uploadRequest.UploadedFile).
		WithResponse(uploadRep).
		WithID(uploadRequest.UploadedFile.ID).
		SaveForMultipleClients(uploadRequest.Clients)

	go al.sendFileToClients(uploadRequest)

	response := api.NewSuccessPayload(uploadRep)

	al.writeJSONResponse(w, http.StatusOK, response)
}

func (al *APIListener) handleUploadsWS(w http.ResponseWriter, req *http.Request) {
	uiConn, err := apiUpgrader.Upgrade(w, req, nil)
	if err != nil {
		al.Errorf("Failed to establish WS connection: %v", err)
		return
	}

	connID, err := uuid.NewUUID()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	al.Server.uploadWebSockets.Store(connID, uiConn)

	defer al.Server.uploadWebSockets.Delete(connID)
	defer func() {
		if cerr := uiConn.Close(); cerr != nil {
			al.Errorf("error closing the upload websocket: %v", cerr)
		}
	}()

	for {
		_, _, err := uiConn.ReadMessage()
		if err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) {
				al.Infof("closed ws connection: %v", err)
			}
			break
		}
	}
}

func (al *APIListener) genFilePath(uuid string) string {
	// uuid may originate from the caller-supplied multipart "id" field; use only the
	// base name so a value containing path separators cannot escape the upload dir.
	uniqueFilename := fmt.Sprintf("%s_proxiport_filepush", filepath.Base(uuid))

	return filepath.Join(al.config.GetUploadDir(), uniqueFilename)
}

type uploadResult struct {
	resp   *models.UploadResponse
	err    error
	client *clientdata.Client
}

type UploadOutput struct {
	ClientID string `json:"client_id"`
	*models.UploadResponse
}

func (al *APIListener) sendFileToClients(uploadRequest *UploadRequest) {
	wg := &sync.WaitGroup{}
	wg.Add(len(uploadRequest.Clients))

	resChan := make(chan *uploadResult, len(uploadRequest.Clients))

	for _, cl := range uploadRequest.Clients {
		go al.sendFileToClient(wg, uploadRequest.UploadedFile, cl, resChan)
	}

	go func() {
		wg.Wait()
		close(resChan)
	}()

	al.consumeUploadResults(resChan, uploadRequest)

	err := al.filesAPI.Remove(uploadRequest.SourceFilePath)
	if err != nil {
		al.Errorf("failed to delete temp file path %s: %v", uploadRequest.SourceFilePath, err)
	}
}

func (al *APIListener) consumeUploadResults(resChan chan *uploadResult, uploadRequest *UploadRequest) {
	for res := range resChan {
		clientID := res.client.GetID()
		output := &UploadOutput{
			ClientID:       clientID,
			UploadResponse: res.resp,
		}
		if res.err != nil {
			errTxt := res.err.Error()
			if errTxt == "client error: unknown request" {
				errTxt = "client doens't support uploads, please upgrade client to the latest version to make it work"
			}
			output.UploadResponse = &models.UploadResponse{
				Message: errTxt,
				Status:  "error",
				UploadResponseShort: models.UploadResponseShort{
					ID: uploadRequest.ID,
				},
			}
			al.Errorf(
				"upload failure: %s, file id: %s, file path: %s, client %s",
				errTxt,
				uploadRequest.ID,
				uploadRequest.DestinationPath,
				clientID,
			)
			al.auditLog.Entry(auditlog.ApplicationUploads, auditlog.ActionFailed).
				WithRequest(uploadRequest.UploadedFile).
				WithResponse(output).
				WithID(uploadRequest.UploadedFile.ID).
				WithClient(res.client).
				Save()
		} else {
			al.Infof(
				"upload success, file id: %s, file path: %s, client %s",
				uploadRequest.ID,
				uploadRequest.DestinationPath,
				clientID,
			)
			al.auditLog.Entry(auditlog.ApplicationUploads, auditlog.ActionSuccess).
				WithRequest(uploadRequest.UploadedFile).
				WithResponse(output).
				WithID(uploadRequest.UploadedFile.ID).
				WithClient(res.client).
				Save()
		}

		al.notifyUploadEventListeners(output)
	}
}

func (al *APIListener) sendFileToClient(wg *sync.WaitGroup, file *models.UploadedFile, cl *clientdata.Client, resChan chan *uploadResult) {
	defer wg.Done()

	fileReceptionConfig := cl.GetFileReceptionConfig()
	if fileReceptionConfig != nil && !fileReceptionConfig.Enabled {
		resChan <- &uploadResult{
			err:    errors3.ErrUploadsDisabled,
			client: cl,
			resp:   nil,
		}
		return
	}
	// The agent fetches the staged file back over SFTP on its own transport.
	// Entitle it to exactly this path, for exactly as long as this request is
	// outstanding, and to nothing else.
	release := al.stagedUploads.Allow(cl.GetID(), file.SourceFilePath)
	defer release()

	conn := cl.GetConnection()
	if !cl.IsConnected() || conn == nil {
		// A disconnected client is reachable here: the target list is built with
		// GetByID, which filters obsolete clients but not disconnected ones, and
		// every client is disconnected right after a restart. Sending anyway
		// dereferenced a nil ssh.Conn in a detached goroutine, which is a
		// crash of the whole daemon rather than a failed upload.
		resChan <- &uploadResult{
			err:    errors3.ErrClientNotConnected,
			client: cl,
			resp:   nil,
		}
		return
	}

	resp := &models.UploadResponse{}
	err := comm.SendRequestAndGetResponse(conn, comm.RequestTypeUpload, file, resp, al.Log())

	resChan <- &uploadResult{
		err:    err,
		client: cl,
		resp:   resp,
	}
}

func (al *APIListener) notifyUploadEventListeners(msg interface{}) {
	al.uploadWebSockets.Range(func(key, value interface{}) bool {
		if wsConn, ok := value.(*websocket.Conn); ok {
			err := wsConn.WriteJSON(msg)
			if err != nil {
				al.Errorf("failed to send notification to websocket client %s: %v", key, err)
			}
		}
		return true
	})
}

func (al *APIListener) uploadRequestFromRequest(req *http.Request) (ur *UploadRequest, err error) {
	ur = &UploadRequest{
		UploadedFile: &models.UploadedFile{},
	}

	// The body is already bounded: the file-push routes are wrapped in
	// middleware.MaxBytes with [api] max_filepush_size, which replaces req.Body
	// with an http.MaxBytesReader before this runs. uploadBufSize here is the
	// in-memory threshold, not the cap.
	err = req.ParseMultipartForm(uploadBufSize) //nolint:gosec // G120: bounded by middleware.MaxBytes on the route
	if err != nil {
		return nil, &errors2.APIError{
			Err:        err,
			HTTPStatus: http.StatusBadRequest,
		}
	}

	ur.ClientIDs = req.MultipartForm.Value["client_id"]
	ur.GroupIDs = req.MultipartForm.Value["group_id"]

	clientTags, err := getClientTagsFromReqForm(req)
	if err != nil {
		return nil, &errors2.APIError{
			Err:        err,
			HTTPStatus: http.StatusBadRequest,
		}
	}

	ur.ClientTags = clientTags

	orderedClients, clientsInGroupsCount, err := al.getOrderedClientsWithValidation(req.Context(), ur)
	if err != nil {
		return nil, err
	}

	ur.Clients = orderedClients
	ur.clientsInGroupsCount = clientsInGroupsCount

	err = ur.FromMultipartRequest(req)
	if err != nil {
		return nil, &errors2.APIError{
			Err:        err,
			HTTPStatus: http.StatusBadRequest,
		}
	}

	ur.File, ur.FileHeader, err = req.FormFile("upload")
	if err != nil {
		return nil, &errors2.APIError{
			Err:        err,
			HTTPStatus: http.StatusBadRequest,
		}
	}

	if ur.ID == "" {
		id, e := random.UUID4()
		if e != nil {
			al.Errorf("failed to generate uuid, will fallback to timestamp uuid, error: %v", e)
			id = fmt.Sprintf("%d", time.Now().UnixNano())
		}
		ur.ID = id
	}

	return ur, nil
}

func getClientTagsFromReqForm(req *http.Request) (clientTags *models.JobClientTags, err error) {
	jsonTags := req.MultipartForm.Value["tags"]

	if len(jsonTags) == 0 {
		return nil, nil
	}

	if len(jsonTags) > 1 {
		return nil, errors.New("tags form value must only contain a single element")
	}

	clientTags = &models.JobClientTags{}
	err = json.Unmarshal([]byte(jsonTags[0]), clientTags)
	if err != nil {
		return nil, err
	}

	return clientTags, nil
}

// deniedDestinationPrefixes is the server's own refusal list for file pushes.
//
// It is a convenience, not the control: the agent enforces its own
// [file-reception] protected list, which is the enforcement that matters
// because the agent is the side whose filesystem is at stake and the server may
// be the hostile party. This list exists so an obviously bad destination is
// refused with a clear error before a file is transferred, and it deliberately
// names the same routes to root the agent list covers -- schedules, boot units,
// shells, authentication -- rather than only pseudo-filesystems.
var deniedDestinationPrefixes = []string{
	"/proc/", "/sys/", "/dev/", "/run/",
	"/etc/cron.d/", "/etc/cron.hourly/", "/etc/cron.daily/",
	"/etc/cron.weekly/", "/etc/cron.monthly/", "/var/spool/cron/",
	"/etc/sudoers.d/", "/etc/pam.d/", "/etc/security/", "/etc/ssh/",
	"/etc/systemd/", "/usr/lib/systemd/", "/lib/systemd/", "/etc/init.d/",
	"/etc/profile.d/", "/etc/ld.so.conf.d/",
	"/etc/apt/apt.conf.d/", "/etc/update-motd.d/",
	"/etc/proxiport/", "/root/",
}

var deniedDestinationPaths = []string{
	"/etc/crontab", "/etc/rc.local", "/etc/profile", "/etc/environment",
	"/etc/passwd", "/etc/shadow", "/etc/group", "/etc/gshadow",
	"/etc/sudoers", "/etc/ld.so.preload",
}

func validateRemoteDestination(ur *UploadRequest) error {
	destination := path.Clean(ur.DestinationPath)

	for _, prefix := range deniedDestinationPrefixes {
		if strings.HasPrefix(destination+"/", prefix) || strings.HasPrefix(destination, prefix) {
			return fmt.Errorf("uploads to %s are forbidden", prefix)
		}
	}
	for _, denied := range deniedDestinationPaths {
		if destination == denied {
			return fmt.Errorf("uploads to %s are forbidden", denied)
		}
	}

	// An SSH authorized-keys file is a login, wherever it lives.
	if strings.Contains(destination, "/.ssh/") {
		return errors.New("uploads into an .ssh directory are forbidden")
	}

	return nil
}
