package chserver

import (
	"errors"
	"net/http"

	"github.com/proximile/proxiport/server/api"
	"github.com/proximile/proxiport/server/auditlog"
)

// handleListAuditLog handles GET /auditlog
func (al *APIListener) handleListAuditLog(w http.ResponseWriter, req *http.Request) {
	curUser, err := al.getUserModelForAuth(req.Context())
	if err != nil {
		al.jsonError(w, err)
		return
	}
	result, err := al.auditLog.List(req, curUser)
	if err != nil {
		var nae *auditlog.NotAllowedError
		if errors.As(err, &nae) {
			al.jsonErrorResponseWithError(w, http.StatusForbidden, "filter forbidden", err)
			return
		}
		al.jsonError(w, err)
		return
	}
	al.writeJSONResponse(w, http.StatusOK, result)
}

// handleVerifyAuditLog handles GET /auditlog/verify — it walks the tamper-evidence
// chain and reports whether every entry's keyed HMAC and link are intact.
func (al *APIListener) handleVerifyAuditLog(w http.ResponseWriter, req *http.Request) {
	res, err := al.auditLog.Verify(req.Context())
	if err != nil {
		al.jsonError(w, err)
		return
	}
	al.writeJSONResponse(w, http.StatusOK, api.NewSuccessPayload(res))
}
