package control

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
)

func JSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
func Failure(w http.ResponseWriter, e error) {
	status := http.StatusBadRequest
	var ce *Error
	if errors.As(e, &ce) {
		status = ce.Status
	}
	JSON(w, status, map[string]string{"error": e.Error()})
}
func Decode(w http.ResponseWriter, r *http.Request, v any) error {
	if ct := r.Header.Get("Content-Type"); ct != "application/json" {
		return &Error{415, "expected application/json"}
	}
	d := json.NewDecoder(http.MaxBytesReader(w, r.Body, 512<<10))
	d.DisallowUnknownFields()
	if e := d.Decode(v); e != nil {
		return e
	}
	var extra any
	if e := d.Decode(&extra); e != io.EOF {
		return &Error{400, "expected one JSON value"}
	}
	return nil
}
func Handler(b Backend, a *Auth, owner, proxy string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Content-Security-Policy", "default-src 'none'; frame-ancestors 'none'")
		token, scheme := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
		if !scheme || len(r.Header.Values("Authorization")) != 1 {
			Failure(w, &Error{401, "management credential required"})
			return
		}
		isOwner := Equal(token, owner)
		if !isOwner && !Equal(token, proxy) {
			Failure(w, &Error{401, "management credential required"})
			return
		}
		if !isOwner {
			if r.Host != "raymond.test" && r.Host != "raymond.test:80" {
				Failure(w, &Error{403, "invalid dashboard host"})
				return
			}
			if origin := r.Header.Get("Origin"); origin != "" && origin != Origin {
				Failure(w, &Error{403, "cross-origin request rejected"})
				return
			}
			if site := r.Header.Get("Sec-Fetch-Site"); site != "" && site != "same-origin" && site != "none" {
				Failure(w, &Error{403, "cross-site request rejected"})
				return
			}
			if r.Method != "GET" && r.Method != "HEAD" && r.Header.Get("Origin") != Origin {
				Failure(w, &Error{403, "dashboard origin required"})
				return
			}
		}
		if r.URL.RawQuery != "" && r.URL.Path != Prefix+"logs" {
			Failure(w, &Error{400, "unknown management query"})
			return
		}
		if r.URL.Path == Prefix+"launch" {
			if !isOwner || r.Method != "POST" {
				Failure(w, &Error{403, "open the dashboard with raymond dashboard"})
				return
			}
			token, e := a.Launch()
			if e != nil {
				Failure(w, e)
				return
			}
			JSON(w, 200, map[string]string{"token": token})
			return
		}
		if r.URL.Path == Prefix+"session" && r.Method == "POST" {
			var in struct {
				Token string `json:"token"`
			}
			if e := Decode(w, r, &in); e != nil {
				Failure(w, e)
				return
			}
			session, csrf, e := a.Exchange(in.Token)
			if e != nil {
				Failure(w, &Error{401, e.Error()})
				return
			}
			JSON(w, 200, map[string]string{"session": session, "csrf": csrf})
			return
		}
		csrf := ""
		if !isOwner {
			sessions := r.Header.Values(SessionHeader)
			if len(sessions) != 1 {
				Failure(w, &Error{401, "open this browser with raymond dashboard"})
				return
			}
			var ok bool
			csrf, ok = a.Session(sessions[0])
			if !ok {
				Failure(w, &Error{401, "session expired; run raymond dashboard"})
				return
			}
			if r.Method != "GET" && r.Method != "HEAD" && !Equal(r.Header.Get("X-Raymond-CSRF"), csrf) {
				Failure(w, &Error{403, "invalid dashboard request token"})
				return
			}
		}
		switch {
		case r.URL.Path == Prefix+"session" && r.Method == "GET":
			JSON(w, 200, map[string]string{"csrf": csrf})
		case r.URL.Path == Prefix+"state" && r.Method == "GET":
			s, e := b.State(r.Context())
			if e != nil {
				Failure(w, e)
				return
			}
			JSON(w, 200, s)
		case r.URL.Path == Prefix+"operations" && r.Method == "POST":
			var in Request
			if e := Decode(w, r, &in); e != nil {
				Failure(w, e)
				return
			}
			if e := validateRequest(in); e != nil {
				Failure(w, e)
				return
			}
			op, e := b.Submit(r.Context(), in)
			if e != nil {
				Failure(w, e)
				return
			}
			JSON(w, http.StatusAccepted, op)
		case strings.HasPrefix(r.URL.Path, Prefix+"operations/") && r.Method == "GET":
			op, e := b.GetOperation(strings.TrimPrefix(r.URL.Path, Prefix+"operations/"))
			if e != nil {
				Failure(w, e)
				return
			}
			JSON(w, 200, op)
		case r.URL.Path == Prefix+"logs" && r.Method == "GET":
			b.Logs(r.Context(), w, r)
		default:
			Failure(w, &Error{404, "unknown management endpoint or method"})
		}
	})
}
