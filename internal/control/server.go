package control

import (
	"context"
	"net"
	"net/http"
	"strings"
	"time"
)

type peerKey struct{}

// UnixServer binds owner credentials to the enrolled OS account, including when
// another account has learned an owner token. The restricted proxy uses its own token.
func UnixServer(handler http.Handler, owner string, uid int) *http.Server {
	return &http.Server{ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 10 * time.Second, IdleTimeout: 30 * time.Second, MaxHeaderBytes: 32 << 10,
		ConnContext: func(ctx context.Context, c net.Conn) context.Context {
			peer := -1
			if u, ok := c.(*net.UnixConn); ok {
				if raw, e := u.SyscallConn(); e == nil {
					_ = raw.Control(func(fd uintptr) { peer, _ = peerUID(fd) })
				}
			}
			return context.WithValue(ctx, peerKey{}, peer)
		}, Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if Equal(strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer "), owner) {
				peer, ok := r.Context().Value(peerKey{}).(int)
				if !ok || (peer != uid && peer != 0) {
					Failure(w, &Error{403, "management belongs to another local account"})
					return
				}
			}
			handler.ServeHTTP(w, r)
		})}
}
