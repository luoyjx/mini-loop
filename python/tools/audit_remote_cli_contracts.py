"""Exercise Source's remote audit CLI through actual loopback HTTP."""

import contextlib
import io
import json
import threading
from http.server import BaseHTTPRequestHandler, HTTPServer

from mini_loop.audit import main
from tools.audit_posture_contracts import audit_posture_contracts


def audit_remote_cli_contracts(scratch) -> dict:
    reports = audit_posture_contracts(scratch)["cases"]
    current = {}
    requests = []

    class Handler(BaseHTTPRequestHandler):
        def do_GET(self):
            requests.append(dict(path=self.path, authorization=self.headers.get("Authorization")))
            body = json.dumps(current["report"]).encode()
            self.send_response(200)
            self.send_header("Content-Type", "application/json")
            self.send_header("Content-Length", str(len(body)))
            self.end_headers()
            self.wfile.write(body)

        def log_message(self, *_):
            pass

    rows = []
    with HTTPServer(("127.0.0.1", 0), Handler) as server:
        thread = threading.Thread(target=server.serve_forever)
        thread.start()
        base = f"http://127.0.0.1:{server.server_port}"
        try:
            for row in reports:
                current["report"] = dict(row["report"], build="fixture-build", pid=17)
                args = ["--url", base + "///"]
                output = io.StringIO()
                with contextlib.redirect_stdout(output):
                    code = main(args)
                rows.append(dict(name=row["name"], report=current["report"],
                                 stdout=output.getvalue().replace(base, "http://fixture.invalid"),
                                 exit_code=code, request=requests[-1]))
        finally:
            server.shutdown()
            thread.join()
    missing = io.StringIO()
    with contextlib.redirect_stdout(missing):
        code = main(["--url"])
    return dict(source="http://fixture.invalid", cases=rows,
                missing_url=dict(exit_code=code, stdout=missing.getvalue()))
