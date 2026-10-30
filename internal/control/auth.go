package control

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

type session struct {
	CSRF    string    `json:"csrf"`
	Expires time.Time `json:"expires"`
}
type authData struct {
	Sessions map[string]session   `json:"sessions"`
	Launches map[string]time.Time `json:"launches"`
}
type Auth struct {
	mu   sync.Mutex
	path string
	data authData
}

func hash(s string) string { b := sha256.Sum256([]byte(s)); return hex.EncodeToString(b[:]) }
func Equal(a, b string) bool {
	return len(a) > 0 && subtle.ConstantTimeCompare([]byte(a), []byte(b)) == 1
}
func NewAuth(path string) (*Auth, error) {
	a := &Auth{path: path, data: authData{Sessions: map[string]session{}, Launches: map[string]time.Time{}}}
	if b, e := os.ReadFile(path); e == nil {
		if e = json.Unmarshal(b, &a.data); e != nil {
			return nil, e
		}
	} else if !os.IsNotExist(e) {
		return nil, e
	}
	if a.data.Sessions == nil {
		a.data.Sessions = map[string]session{}
	}
	if a.data.Launches == nil {
		a.data.Launches = map[string]time.Time{}
	}
	return a, nil
}
func AtomicJSON(path string, v any) error {
	b, e := json.Marshal(v)
	if e != nil {
		return e
	}
	return AtomicWrite(path, b, 0600)
}
func AtomicWrite(path string, b []byte, mode os.FileMode) error {
	f, e := os.CreateTemp(filepath.Dir(path), ".raymond-*")
	if e != nil {
		return e
	}
	defer os.Remove(f.Name())
	if e = f.Chmod(mode); e == nil {
		_, e = f.Write(b)
	}
	if e == nil {
		e = f.Sync()
	}
	ce := f.Close()
	if e == nil {
		e = ce
	}
	if e != nil {
		return e
	}
	if e = os.Rename(f.Name(), path); e != nil {
		return e
	}
	d, e := os.Open(filepath.Dir(path))
	if e != nil {
		return e
	}
	defer d.Close()
	return d.Sync()
}
func (a *Auth) prune() {
	now := time.Now()
	for k, v := range a.data.Launches {
		if now.After(v) {
			delete(a.data.Launches, k)
		}
	}
	for k, v := range a.data.Sessions {
		if now.After(v.Expires) {
			delete(a.data.Sessions, k)
		}
	}
}
func (a *Auth) Launch() (string, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.prune()
	if len(a.data.Launches) >= 32 {
		return "", fmt.Errorf("too many pending dashboard launches")
	}
	token := rand.Text()
	a.data.Launches[hash(token)] = time.Now().Add(time.Minute)
	return token, AtomicJSON(a.path, a.data)
}
func (a *Auth) Exchange(token string) (string, string, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.prune()
	key := hash(token)
	if _, ok := a.data.Launches[key]; !ok {
		return "", "", fmt.Errorf("launch link expired; run raymond dashboard again")
	}
	delete(a.data.Launches, key)
	if len(a.data.Sessions) >= 64 {
		return "", "", fmt.Errorf("too many dashboard sessions")
	}
	cookie, csrf := rand.Text(), rand.Text()
	a.data.Sessions[hash(cookie)] = session{csrf, time.Now().Add(30 * 24 * time.Hour)}
	return cookie, csrf, AtomicJSON(a.path, a.data)
}
func (a *Auth) Session(cookie string) (string, bool) {
	a.mu.Lock()
	defer a.mu.Unlock()
	s, ok := a.data.Sessions[hash(cookie)]
	return s.CSRF, ok && time.Now().Before(s.Expires)
}
