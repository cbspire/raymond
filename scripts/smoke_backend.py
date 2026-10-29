"""Disposable CI helper, never a Raymond runtime dependency."""
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
import threading
import signal


class Backend(BaseHTTPRequestHandler):
    protocol_version = "HTTP/1.1"

    def do_GET(self):
        body = (b"localhost unchanged" if self.server.server_port == 80
                else (self.headers["Host"] + " " + self.path).encode())
        self.send_response(200)
        self.send_header("Content-Length", str(len(body)))
        self.end_headers()
        self.wfile.write(body)

    def log_message(self, *args):
        pass


servers = [ThreadingHTTPServer(("127.0.0.1", p), Backend) for p in (80, 9001, 9002)]
for server in servers:
    threading.Thread(target=server.serve_forever, daemon=True).start()
signal.pause()
