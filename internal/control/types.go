// Package control defines the local management protocol. Its Unix transport is
// separate from the browser session: the proxy never forwards owner credentials.
package control

import (
	"context"
	"encoding/json"
	"net/http"
	"time"

	"github.com/cbspire/raymond/internal/config"
)

const RootDir = "/var/lib/raymond/management"
const SocketDir = "/var/run/raymond-control"
const SocketPath = SocketDir + "/control.sock"
const Prefix = "/api/v1/"
const Origin = "http://raymond.test"
const CookieName = "raymond_session"

type Metadata struct {
	UID        int    `json:"uid"`
	GID        int    `json:"gid"`
	Home       string `json:"home"`
	ConfigPath string `json:"config_path"`
	CLIPath    string `json:"cli_path"`
	OwnerToken string `json:"owner_token"`
	ProxyToken string `json:"proxy_token"`
}
type ConfigView struct {
	Config   config.Config `json:"config"`
	Revision string        `json:"revision"`
	Error    string        `json:"error,omitempty"`
}
type State struct {
	Version   string     `json:"version"`
	Platform  string     `json:"platform"`
	Running   bool       `json:"running"`
	Saved     ConfigView `json:"saved"`
	Active    ConfigView `json:"active"`
	Pending   bool       `json:"pending"`
	Operation *Operation `json:"operation,omitempty"`
}
type Request struct {
	Action   string         `json:"action"`
	Config   *config.Config `json:"config,omitempty"`
	Revision string         `json:"revision,omitempty"`
	Version  string         `json:"version,omitempty"`
}
type Operation struct {
	ID      string          `json:"id"`
	Action  string          `json:"action"`
	Status  string          `json:"status"`
	Phase   string          `json:"phase,omitempty"`
	Error   string          `json:"error,omitempty"`
	Result  json.RawMessage `json:"result,omitempty"`
	Created time.Time       `json:"created"`
	Updated time.Time       `json:"updated"`
	Request Request         `json:"-"`
}
type Backend interface {
	State(context.Context) (State, error)
	Submit(context.Context, Request) (Operation, error)
	GetOperation(string) (Operation, error)
	Logs(context.Context, http.ResponseWriter, *http.Request)
}

type Error struct {
	Status  int
	Message string
}

func (e *Error) Error() string { return e.Message }
func Conflict(s string) error  { return &Error{http.StatusConflict, s} }
