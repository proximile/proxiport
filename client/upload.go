package chclient

import (
	"fmt"
	"io"
	"os"
	"os/user"
	"path/filepath"
	"strings"

	errors2 "github.com/proximile/proxiport/share/errors"

	"github.com/proximile/proxiport/client/system"

	"github.com/pkg/sftp"

	"github.com/pkg/errors"

	"golang.org/x/crypto/ssh"

	"github.com/proximile/proxiport/share/files"
	"github.com/proximile/proxiport/share/logger"
	"github.com/proximile/proxiport/share/models"
)

// destinationDirMode is the mode of directories the agent creates to hold a
// pushed file.
const destinationDirMode = os.FileMode(0o755)

type SourceFileProvider interface {
	Open(path string) (io.ReadCloser, error)
}

type OptionsProvider interface {
	GetUploadDir() string
	GetProtectedUploadDirs() []string
	IsFileReceptionEnabled() bool
}

type UploadManager struct {
	*logger.Logger
	FilesAPI           files.FileAPI
	OptionsProvider    OptionsProvider
	SourceFileProvider SourceFileProvider
	SysUserLookup      system.SysUserLookup
}

type SSHFileProvider struct {
	sshConn ssh.Conn
}

type SftpSession struct {
	RemoteFile io.ReadCloser
	SftpCl     *sftp.Client
}

func (ss *SftpSession) Read(p []byte) (n int, err error) {
	return ss.RemoteFile.Read(p)
}

func (ss *SftpSession) Close() error {
	errs := make([]string, 0, 2)

	err := ss.RemoteFile.Close()
	if err != nil {
		errs = append(errs, err.Error())
	}

	err = ss.SftpCl.Close()
	if err != nil {
		errs = append(errs, err.Error())
	}

	if len(errs) == 0 {
		return nil
	}

	return errors.New(strings.Join(errs, ", "))
}

func (sfp SSHFileProvider) Open(path string) (io.ReadCloser, error) {
	conn := ssh.NewClient(sfp.sshConn, nil, nil)
	sftpCl, err := sftp.NewClient(conn)
	if err != nil {
		return nil, errors.Wrapf(err, "failed to establish sftp connection to the server")
	}

	sftpFile, err := sftpCl.Open(path)

	if err != nil {
		return nil, err
	}

	return &SftpSession{
		RemoteFile: sftpFile,
		SftpCl:     sftpCl,
	}, nil
}

func NewSSHUploadManager(
	l *logger.Logger,
	filesAPI files.FileAPI,
	optionsProvider OptionsProvider,
	sshConn ssh.Conn,
	sysUserLookup system.SysUserLookup,
) *UploadManager {
	sshFileProvider := &SSHFileProvider{
		sshConn: sshConn,
	}
	return &UploadManager{
		Logger:             l,
		FilesAPI:           filesAPI,
		OptionsProvider:    optionsProvider,
		SourceFileProvider: sshFileProvider,
		SysUserLookup:      sysUserLookup,
	}
}

func (um *UploadManager) HandleUploadRequest(reqPayload []byte) (*models.UploadResponse, error) {
	if !um.OptionsProvider.IsFileReceptionEnabled() {
		err := errors2.ErrUploadsDisabled
		um.Debugf(err.Error())
		return nil, err
	}

	um.Debugf("got request %s", string(reqPayload))

	uploadedFile, err := um.getUploadedFile(reqPayload)
	if err != nil {
		return nil, err
	}

	destinationFileExists, err := um.FilesAPI.Exist(uploadedFile.DestinationPath)
	if err != nil {
		return nil, err
	}

	if !destinationFileExists || uploadedFile.ForceWrite {
		return um.handleWritingFile(uploadedFile)
	}

	fileShouldBeSynched, err := um.fileShouldBeSynched(uploadedFile)
	if err != nil {
		return nil, err
	}
	if fileShouldBeSynched {
		um.Debugf("file %s should be synched", uploadedFile.DestinationPath)
		return um.handleWritingFile(uploadedFile)
	}

	msg := fmt.Sprintf("file %s already exists, should not be synched or overwritten with force", uploadedFile.DestinationPath)
	um.Infof(msg)
	return &models.UploadResponse{
		UploadResponseShort: models.UploadResponseShort{
			ID:       uploadedFile.ID,
			Filepath: uploadedFile.DestinationPath,
		},
		Message: msg,
		Status:  "ignored",
	}, nil
}

func (um *UploadManager) fileShouldBeSynched(uploadedFile *models.UploadedFile) (bool, error) {
	if !uploadedFile.Sync {
		return false, nil
	}

	hashSumMatch, err := files.Md5HashMatch(uploadedFile.Md5Checksum, uploadedFile.DestinationPath, um.FilesAPI)

	if err != nil {
		return false, err
	}
	if !hashSumMatch {
		um.Debugf(
			"destination file %s has a different hash sum than the provided %x, the file should be synched",
			uploadedFile.DestinationPath,
			uploadedFile.Md5Checksum,
		)

		return true, nil
	}

	if uploadedFile.DestinationFileMode > 0 {
		destinationFileMode, err := um.FilesAPI.GetFileMode(uploadedFile.DestinationPath)
		if err != nil {
			return false, err
		}

		if destinationFileMode != uploadedFile.DestinationFileMode {
			um.Debugf(
				"destination file %s has a different file mode than the provided %v, the file should be synched",
				uploadedFile.DestinationPath,
				uploadedFile.DestinationFileMode,
			)

			return true, nil
		}
	}

	if uploadedFile.DestinationFileOwner != "" || uploadedFile.DestinationFileGroup != "" {
		fileOwnerMatch, err := um.fileOwnerOrGroupMatch(
			uploadedFile.DestinationPath,
			uploadedFile.DestinationFileOwner,
			uploadedFile.DestinationFileGroup,
		)
		if err != nil {
			return false, err
		}

		if !fileOwnerMatch {
			um.Debugf(
				"destination file %s has a different file owner or group than the provided ones %s:%s, ",
				uploadedFile.DestinationPath,
				uploadedFile.DestinationFileOwner,
				uploadedFile.DestinationFileGroup,
			)

			return true, nil
		}
	}

	return false, nil
}

func (um *UploadManager) handleWritingFile(uploadedFile *models.UploadedFile) (*models.UploadResponse, error) {
	copiedBytes, tempFilePath, err := um.copyFileToTempLocation(
		uploadedFile.SourceFilePath,
		uploadedFile.Md5Checksum,
	)
	if err != nil {
		return nil, err
	}

	// From here on the staged payload is removed unless it reaches the
	// destination, so a failed push leaves nothing behind in the upload dir.
	moved := false
	defer func() {
		if !moved {
			um.removeStagedFile(tempFilePath)
		}
	}()

	// A mode or owner that cannot be applied fails the push rather than
	// writing the file with the wrong permissions and reporting success.
	err = um.chmodFile(tempFilePath, uploadedFile.DestinationFileMode)
	if err != nil {
		return nil, fmt.Errorf("chmod of %s failed: %w", uploadedFile.DestinationPath, err)
	}

	err = um.chownFile(tempFilePath, uploadedFile.DestinationFileOwner, uploadedFile.DestinationFileGroup)
	if err != nil {
		return nil, fmt.Errorf(
			"chown of %s to %s:%s failed: %w",
			uploadedFile.DestinationPath,
			uploadedFile.DestinationFileOwner,
			uploadedFile.DestinationFileGroup,
			err,
		)
	}

	err = um.moveFileToDestination(tempFilePath, uploadedFile)
	if err != nil {
		return nil, err
	}
	moved = true
	um.Debugf("copied remote file %s to the temp path %s", uploadedFile.SourceFilePath, tempFilePath)

	message := "file successfully copied to destination " + uploadedFile.DestinationPath
	um.Debugf(message)

	return &models.UploadResponse{
		UploadResponseShort: models.UploadResponseShort{
			ID:        uploadedFile.ID,
			Filepath:  uploadedFile.DestinationPath,
			SizeBytes: copiedBytes,
		},
		Status:  "success",
		Message: message,
	}, nil
}

func (um *UploadManager) removeStagedFile(tempFilePath string) {
	err := um.FilesAPI.Remove(tempFilePath)
	if err != nil && !os.IsNotExist(err) {
		um.Errorf("failed to remove %s: %v", tempFilePath, err)
	}
}

func (um *UploadManager) chownFile(filePath, owner, group string) (err error) {
	if owner == "" && group == "" {
		return nil
	}
	curUser, curGroup, err := um.SysUserLookup.GetCurrentUserAndGroup()
	if err != nil {
		return err
	}

	if um.ownerOrGroupMatchesCurrentUser(owner, group, curUser, curGroup) {
		um.Debugf(
			"given user %s and/or group %s match current user %s and/or group %s therefore no chown is needed",
			owner,
			group,
			curUser.Username,
			curGroup.Name,
		)
		return nil
	}

	err = um.FilesAPI.ChangeOwner(filePath, owner, group)
	if err != nil {
		return err
	}

	um.Debugf(
		"chowned file %s, %s:%s",
		filePath,
		owner,
		group,
	)

	return nil
}

func (um *UploadManager) ownerOrGroupMatchesCurrentUser(targetOwner, targetGroup string, currentUser *user.User, currentGroup *user.Group) bool {
	if targetOwner != "" && currentUser.Username == targetOwner && targetGroup == "" {
		return true
	}

	if targetOwner == "" && targetGroup != "" && currentGroup.Name == targetGroup {
		return true
	}

	return currentUser.Username == targetOwner && currentGroup.Name == targetGroup
}

func (um *UploadManager) chmodFile(path string, mode os.FileMode) (err error) {
	if mode == 0 {
		return nil
	}

	// Defense in depth behind models.UploadedFile.Validate, which the agent
	// runs on every incoming request. This is the call that would actually set
	// a setuid bit, so it refuses one too.
	if mode&^models.MaxPushedFileMode != 0 {
		return fmt.Errorf(
			"refusing to set mode %#o on a pushed file: only permission bits up to %#o may be set",
			mode, models.MaxPushedFileMode)
	}

	err = um.FilesAPI.ChangeMode(path, mode)
	if err != nil {
		return err
	}

	um.Debugf(
		"chmoded file %s: %v",
		path,
		mode,
	)

	return nil
}

func (um *UploadManager) moveFileToDestination(tempFilePath string, uploadedFile *models.UploadedFile) (err error) {
	createdDirs, err := um.prepareDestinationDir(uploadedFile.DestinationPath)
	if err != nil {
		return err
	}
	defer func() {
		if err != nil {
			um.removeCreatedDirs(createdDirs)
		}
	}()

	destinationFileExists, err := um.FilesAPI.Exist(uploadedFile.DestinationPath)
	if err != nil {
		return err
	}
	if destinationFileExists {
		var destinationMode os.FileMode
		destinationMode, err = um.FilesAPI.GetFileMode(uploadedFile.DestinationPath)
		if err != nil {
			return err
		}
		// A regular file is replaced by the rename itself, so it is never
		// deleted first: a move that fails leaves the old file in place.
		if !destinationMode.IsRegular() {
			um.Debugf("destination %s already exists, will delete it", uploadedFile.DestinationPath)
			err = um.FilesAPI.Remove(uploadedFile.DestinationPath)
			if err != nil {
				return err
			}
		}
	}

	err = um.FilesAPI.Rename(tempFilePath, uploadedFile.DestinationPath)
	if err != nil {
		return err
	}

	return nil
}

// prepareDestinationDir creates the destination's directory if it is missing
// and returns the directories it created, deepest first, so a push that fails
// afterwards can remove them again. Directories get destinationDirMode, never
// the pushed file's mode: a 0644 directory cannot be entered by anyone but root.
func (um *UploadManager) prepareDestinationDir(destinationPath string) (createdDirs []string, err error) {
	destinationDir := filepath.Dir(destinationPath)

	for dir := destinationDir; ; {
		exists, existErr := um.FilesAPI.Exist(dir)
		if existErr != nil {
			return nil, existErr
		}
		if exists {
			break
		}
		createdDirs = append(createdDirs, dir)

		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}

	if len(createdDirs) == 0 {
		return nil, nil
	}

	_, err = um.FilesAPI.CreateDirIfNotExists(destinationDir, destinationDirMode)
	if err != nil {
		// MkdirAll may have got part of the way.
		um.removeCreatedDirs(createdDirs)
		return nil, err
	}
	um.Debugf("created destination file dir %s for the uploaded file", destinationDir)

	return createdDirs, nil
}

// removeCreatedDirs removes directories a failed push created, deepest first.
// Remove refuses a directory that is not empty, so nothing else is lost.
func (um *UploadManager) removeCreatedDirs(createdDirs []string) {
	for _, dir := range createdDirs {
		err := um.FilesAPI.Remove(dir)
		if err != nil && !os.IsNotExist(err) {
			um.Errorf("failed to remove directory %s: %v", dir, err)
			return
		}
	}
}

func (um *UploadManager) copyFileToTempLocation(remoteFilePath string, expectedMd5Checksum []byte) (
	bytesCopied int64,
	tempFilePath string,
	err error,
) {
	tempFileName := filepath.Base(remoteFilePath)

	tempDirWasCreated, err := um.FilesAPI.CreateDirIfNotExists(um.OptionsProvider.GetUploadDir(), files.UploadStagingDirMode)
	if err != nil {
		return 0, "", err
	}
	if tempDirWasCreated {
		um.Debugf("created temp dir %s for uploaded files", um.OptionsProvider.GetUploadDir())
	}

	tempFilePath = filepath.Join(um.OptionsProvider.GetUploadDir(), tempFileName)

	tempFileExists, err := um.FilesAPI.Exist(tempFilePath)
	if err != nil {
		return 0, tempFilePath, err
	}

	if tempFileExists {
		um.Debugf("temp file %s already exists, will delete it", tempFilePath)
		err = um.FilesAPI.Remove(tempFilePath)
		if err != nil {
			return 0, tempFilePath, err
		}
	}

	remoteFile, err := um.SourceFileProvider.Open(remoteFilePath)
	if err != nil {
		return 0, tempFilePath, err
	}
	defer func() { _ = remoteFile.Close() }()

	// A partly written or unverified payload is not left in the upload dir.
	defer func() {
		if err != nil {
			um.removeStagedFile(tempFilePath)
		}
	}()

	copiedBytes, err := um.FilesAPI.CreateFile(tempFilePath, remoteFile)
	if err != nil {
		return 0, tempFilePath, err
	}
	um.Debugf("copied %d bytes from server path %s to temp path %s", copiedBytes, remoteFilePath, tempFilePath)

	hashSumMatch, err := files.Md5HashMatch(expectedMd5Checksum, tempFilePath, um.FilesAPI)
	if err != nil {
		return 0, tempFilePath, err
	}

	if !hashSumMatch {
		return 0,
			tempFilePath,
			fmt.Errorf(
				"md5 check failed: checksum from server %x doesn't equal the calculated checksum",
				expectedMd5Checksum,
			)
	}

	return copiedBytes, tempFilePath, nil
}

func (um *UploadManager) getUploadedFile(reqPayload []byte) (*models.UploadedFile, error) {
	uploadedFile := new(models.UploadedFile)
	err := uploadedFile.FromBytes(reqPayload)
	if err != nil {
		return nil, err
	}
	err = uploadedFile.Validate()
	if err != nil {
		return nil, err
	}

	err = uploadedFile.ValidateDestinationPath(um.OptionsProvider.GetProtectedUploadDirs(), um.Logger)
	if err != nil {
		return nil, err
	}

	return uploadedFile, nil
}

func (um *UploadManager) fileOwnerOrGroupMatch(file, owner, group string) (bool, error) {
	fileUID, fileGid, err := um.FilesAPI.GetFileOwnerAndGroup(file)
	if err != nil {
		return false, errors.Wrapf(err, "failed to read uid and gid of file %s", file)
	}

	if owner != "" {
		ownerUID, err := um.SysUserLookup.GetUIDByName(owner)
		if err != nil {
			return false, err
		}

		if fileUID != ownerUID {
			return false, nil
		}
	}

	if group != "" {
		ownerGid, err := um.SysUserLookup.GetGidByName(group)
		if err != nil {
			return false, err
		}

		if fileGid != ownerGid {
			return false, nil
		}
	}

	return true, nil
}
