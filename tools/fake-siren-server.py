#!/usr/bin/env python3
"""A tiny Siren server for developing RESTForge against.

Its vocabulary is deliberately unfamiliar -- there is no status, no job, no
host and no fan anywhere in it. That is the point: RESTForge is meant to render
a Siren API it has never seen, and the only way to demonstrate that is to point
it at one. If a screen here looks wrong, the app has learned something about a
particular server that it should not know.

It also reproduces the failure modes that are awkward to trigger against a real
server: a 401 for a wrong key, a 409, a slow response, and a route that returns
something that is not JSON at all.

    python3 tools/fake-siren-server.py [port]

Then add a backend in the settings page with base URL http://<host>:<port>/
and secret "open-sesame".
"""

import json
import sys
import time
from http.server import BaseHTTPRequestHandler, HTTPServer
from urllib.parse import urlparse, parse_qs

# Not a secret. This server is a fixture; it exists to be pointed at from a
# development phone or emulator and holds nothing worth protecting.
API_KEY = "open-sesame"
AUTH_HEADER = "X-API-Key"

# Mutable so the actions below have something to change, which is what makes
# the "re-fetch after acting" rule observable.
state = {
    "kettle": "cold",
    "brews": 0,
    # started_at is when the current brew began, or None. The job below derives
    # its state from the clock rather than storing it, so a client polling the
    # job sees it genuinely progress -- which is the only way to exercise the
    # polling path for real.
    "job_id": 0,
    "started_at": None,
}

# How the brew progresses. (elapsed seconds, step) in order; past the last
# entry the job is done.
BREW_STEPS = [
    (0, "heating the water"),
    (8, "steeping"),
    (16, "pouring"),
]
BREW_SECONDS = 24

# What a client should assume about staleness if it stops hearing from us.
# A real server derives this from its own timeouts; the point for a client is
# that it comes from the response rather than being hardcoded.
STALE_AFTER_SECONDS = 120


def job_state():
    """The current job, derived from the clock."""
    if state["started_at"] is None:
        return {"state": "none", "id": 0, "step": "", "rc": 0}
    elapsed = time.time() - state["started_at"]
    if elapsed >= BREW_SECONDS:
        return {"state": "done", "id": state["job_id"], "step": "", "rc": 0}
    step = BREW_STEPS[0][1]
    for at, name in BREW_STEPS:
        if elapsed >= at:
            step = name
    return {"state": "running", "id": state["job_id"], "step": step, "rc": 0}


def brewing():
    return job_state()["state"] == "running"


def entity(classes, title, properties, links=None, actions=None, entities=None):
    doc = {"class": classes, "title": title, "properties": properties}
    if links:
        doc["links"] = links
    if actions:
        doc["actions"] = actions
    if entities:
        doc["entities"] = entities
    return doc


def root():
    actions = [
        {
            "name": "brew",
            "title": "Brew a pot of tea",
            "method": "POST",
            "href": "/brew",
            "type": "application/x-www-form-urlencoded",
            "fields": [
                # A field type RESTForge has no special handling for, on
                # purpose: an unknown type must not be an error.
                {"name": "strength", "type": "range", "title": "Strength",
                 "value": "3"},
            ],
        },
    ]
    actions.append({
        # A required field with no default: nothing but the user can supply it,
        # so a client has to ask or refuse. Either is fine; inventing one is not.
        "name": "label-jar",
        "title": "Write a label for a jar",
        "method": "POST",
        "href": "/label",
        "fields": [
            {"name": "text", "type": "text", "required": True,
             "title": "What should the label say?"},
        ],
    })
    if state["kettle"] == "hot" and not brewing():
        actions.append({
            "name": "cool-down",
            "title": "Let the kettle cool",
            "method": "POST",
            "href": "/cool",
            "fields": [
                {"name": "confirm", "type": "checkbox", "required": True,
                 "title": "The kettle is still hot and someone may be waiting "
                          "for it. Cool it down anyway?"},
            ],
        })
    return entity(
        ["pantry"],
        "The pantry",
        {"apiVersion": 1, "version": "fixture-1", "kettle": state["kettle"],
         "brews": state["brews"],
         # Non-ASCII on purpose: a server is free to send it, and a font
         # subset that omits it renders every one of these as a hollow box.
         "unicode": "caf\u00e9 \u2013 na\u00efve \u2014 \u00bd \u20ac \u2018q\u2019"},
        links=[
            {"rel": ["self"], "href": "/"},
            {"rel": ["shelves"], "href": "/shelves"},
            {"rel": ["kettle-job"], "href": "/job"},
            {"rel": ["describedby"], "href": "/doc"},
        ],
        actions=actions,
    )


def shelves():
    return entity(
        ["shelf-list"],
        "Shelves",
        {"count": 3},
        links=[{"rel": ["self"], "href": "/shelves"},
               {"rel": ["up"], "href": "/"}],
        entities=[
            entity(["shelf"], "Top shelf",
                   {"name": "top", "jars": 4, "reachable": True}),
            entity(["shelf"], "Middle shelf",
                   {"name": "middle", "jars": 9, "reachable": True,
                    "note": "a deliberately long value, long enough that it "
                            "has to be truncated or opened in its own window "
                            "rather than fitting on one line of a menu row"}),
            # A sub-entity that is a reference, not an embedded document.
            {"class": ["shelf"], "rel": ["item"], "href": "/shelves/bottom",
             "title": "Bottom shelf"},
        ],
    )


def job():
    return entity(
        ["kettle-job"],
        "Kettle job",
        dict(job_state(), staleAfterSeconds=STALE_AFTER_SECONDS),
        links=[{"rel": ["self"], "href": "/job"}],
    )


class Handler(BaseHTTPRequestHandler):
    def _send(self, status, payload, content_type="application/vnd.siren+json"):
        body = payload if isinstance(payload, bytes) else json.dumps(payload).encode()
        self.send_response(status)
        self.send_header("Content-Type", content_type)
        self.send_header("Content-Length", str(len(body)))
        # A header identifying the answering node, as a load-balanced
        # deployment would have. RESTForge logs every response header rather
        # than looking for a named one, so this is here to be seen in the log.
        self.send_header("X-Fixture-Node", "pantry-1")
        self.end_headers()
        self.wfile.write(body)

    def _authorised(self):
        if self.headers.get(AUTH_HEADER) == API_KEY:
            return True
        self._send(401, {"class": ["error"],
                         "properties": {"message": "API key rejected"}})
        return False

    def do_GET(self):
        path = urlparse(self.path).path
        if not self._authorised():
            return
        if path == "/":
            self._send(200, root())
        elif path in ("/shelves", "/shelves/"):
            self._send(200, shelves())
        elif path == "/job":
            self._send(200, job())
        elif path in ("/slow", "/slow/"):
            # Longer than the 20s read timeout, to exercise the timeout path.
            time.sleep(30)
            self._send(200, root())
        elif path in ("/notjson", "/notjson/"):
            self._send(200, b"<html>not siren at all</html>", "text/html")
        elif path == "/doc":
            self._send(200, b"This fixture has no documentation.", "text/plain")
        else:
            self._send(404, {"class": ["error"],
                             "properties": {"message": "no such thing here"}})

    def do_POST(self):
        path = urlparse(self.path).path
        if not self._authorised():
            return
        length = int(self.headers.get("Content-Length") or 0)
        fields = parse_qs(self.rfile.read(length).decode()) if length else {}

        if path == "/brew":
            if brewing():
                # The race the contract cares about: acting on stale state.
                self._send(409, {"class": ["error"], "properties": {
                    "message": "a brew is already running"}})
                return
            state["brews"] += 1
            state["job_id"] = state["brews"]
            state["kettle"] = "hot"
            state["started_at"] = time.time()
            # 202 with a job entity: the shape a client has to recognise as
            # "accepted, still running" rather than "finished".
            self._send(202, job())
        elif path == "/cool":
            if fields.get("confirm", ["false"])[0] != "true":
                self._send(409, {"class": ["error"], "properties": {
                    "message": "cooling needs confirmation"}})
                return
            state["kettle"] = "cold"
            state["started_at"] = None
            self._send(200, root())
        elif path == "/label":
            text = fields.get("text", [""])[0]
            if not text:
                self._send(409, {"class": ["error"], "properties": {
                    "message": "a label needs some words on it"}})
                return
            state["label"] = text
            self._send(200, root())
        else:
            self._send(404, {"class": ["error"],
                             "properties": {"message": "nothing to do there"}})

    def log_message(self, fmt, *args):
        sys.stderr.write("%s %s\n" % (self.address_string(), fmt % args))


def main():
    port = int(sys.argv[1]) if len(sys.argv) > 1 else 8731
    print("fixture Siren server on http://localhost:%d/  key %r"
          % (port, API_KEY), file=sys.stderr)
    HTTPServer(("", port), Handler).serve_forever()


if __name__ == "__main__":
    main()
