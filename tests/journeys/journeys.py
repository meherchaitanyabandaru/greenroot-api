#!/usr/bin/env python3
"""GreenRoot user journeys, end to end through the public API.

Every journey registers its own fresh users (random mobiles), walks a real
business flow, and checks both the happy path and the rules that must refuse
(wrong role, wrong state, wrong owner, expired or forged tokens…). Where the
rule lives in Redis (OTP, rate limits, token blocklist, caches) the journey
also checks Redis itself.

Runs against any API in mock-SMS mode (OTP 123456) whose database has the
master data and the admin user (9000000000):

    REDIS_ADDR=localhost:6379 JWT_SECRET=local-dev-change-me \\
      python3 tests/journeys/journeys.py [--api URL] [--only auth,orders]

Standard library only. Exit status 1 if any check fails.
"""
import argparse
import base64
import datetime
import hashlib
import hmac
import json
import os
import random
import socket
import sys
import time
import traceback
import urllib.error
import urllib.parse
import urllib.request

# ── setup ────────────────────────────────────────────────────────────────────

parser = argparse.ArgumentParser(description="GreenRoot API journeys")
parser.add_argument("--api", default=os.environ.get("API_URL", "http://localhost:8080/api/v1"))
parser.add_argument("--only", default="", help="comma-separated journey names")
args = parser.parse_args()
API = args.api.rstrip("/")
OTP = "123456"
# Sellers declare the vehicle carries no prohibited items when loading is done.
LOADED_OK = {"declared_no_prohibited_items": True}
ADMIN_MOBILE = "9000000000"

results = {"pass": 0, "fail": 0, "failures": []}
current = {"journey": ""}


class CheckFailed(Exception):
    pass


def check(label, ok, detail=None):
    if ok:
        results["pass"] += 1
        print(f"    ok    {label}")
        return
    results["fail"] += 1
    msg = f"{current['journey']}: {label}"
    results["failures"].append(msg + (f"\n            {json.dumps(detail, default=str)[:400]}" if detail is not None else ""))
    print(f"    FAIL  {label}\n          {json.dumps(detail, default=str)[:400]}")


def require(label, ok, detail=None):
    """Like check, but the rest of the journey depends on it."""
    check(label, ok, detail)
    if not ok:
        raise CheckFailed(label)


def call(method, path, token=None, body=None, headers=None, raw=False):
    data = json.dumps(body).encode() if body is not None else None
    req = urllib.request.Request(API + path, data=data, method=method)
    req.add_header("Content-Type", "application/json")
    for k, v in (headers or {}).items():
        req.add_header(k, v)
    if token:
        req.add_header("Authorization", "Bearer " + token)
    try:
        with urllib.request.urlopen(req, timeout=30) as r:
            payload = r.read()
            return r.status, (payload if raw else json.loads(payload or b"{}"))
    except urllib.error.HTTPError as e:
        payload = e.read()
        try:
            return e.code, json.loads(payload) if payload else {}
        except ValueError:
            return e.code, {"raw": payload[:200].decode(errors="replace")}


def expect(label, method, path, token=None, body=None, status=200, headers=None):
    """Call and check the status; returns the body."""
    code, out = call(method, path, token, body, headers)
    wanted = status if isinstance(status, tuple) else (status,)
    check(f"{label}  [{method} {path} → {code}]", code in wanted, out if code not in wanted else None)
    return out


# A 1x1 PNG: stands in for the camera selfie market sellers must have.
SELFIE_PNG = base64.b64decode("iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mNk+M9QDwADhgGAWjR9awAAAABJRU5ErkJggg==")


def upload_selfie(label, user, data=SELFIE_PNG, want=200):
    """POST /users/me/avatar as multipart; returns the stored photo URL."""
    boundary = "journey" + str(random.randint(10**8, 10**9))
    body = (f'--{boundary}\r\nContent-Disposition: form-data; name="avatar"; filename="selfie.png"\r\n'
            "Content-Type: image/png\r\n\r\n").encode() + data + f"\r\n--{boundary}--\r\n".encode()
    req = urllib.request.Request(API + "/users/me/avatar", data=body, method="POST")
    req.add_header("Content-Type", f"multipart/form-data; boundary={boundary}")
    req.add_header("Authorization", "Bearer " + user.token)
    try:
        with urllib.request.urlopen(req, timeout=30) as r:
            code, out = r.status, json.loads(r.read())
    except urllib.error.HTTPError as e:
        code, out = e.code, {"raw": e.read()[:200].decode(errors="replace")}
    url = (out.get("user") or {}).get("profile_image_url")
    check(f"{label}  [POST /users/me/avatar → {code}]", code == want and (want != 200 or bool(url)), out)
    return url


def err_code(body):
    return (body or {}).get("error", {}).get("code")


# ── Redis (RESP over a socket; only the commands the checks need) ───────────

class Redis:
    def __init__(self, addr):
        host, port = addr.split(":")
        self.addr = (host, int(port))

    def cmd(self, *parts):
        s = socket.create_connection(self.addr, timeout=5)
        try:
            out = b"*%d\r\n" % len(parts) + b"".join(b"$%d\r\n%s\r\n" % (len(str(p).encode()), str(p).encode()) for p in parts)
            s.sendall(out)
            f = s.makefile("rb")
            return self._read(f)
        finally:
            s.close()

    def _read(self, f):
        line = f.readline().rstrip(b"\r\n")
        kind, rest = line[:1], line[1:]
        if kind == b"+":
            return rest.decode()
        if kind == b"-":
            raise RuntimeError(rest.decode())
        if kind == b":":
            return int(rest)
        if kind == b"$":
            n = int(rest)
            if n < 0:
                return None
            data = f.read(n + 2)[:-2]
            return data.decode(errors="replace")
        if kind == b"*":
            n = int(rest)
            return None if n < 0 else [self._read(f) for _ in range(n)]
        raise RuntimeError(f"unexpected redis reply {line!r}")

    def keys(self, pattern):
        cursor, found = "0", []
        while True:
            cursor, batch = self.cmd("SCAN", cursor, "MATCH", pattern, "COUNT", 500)
            found += batch
            if cursor == "0":
                return found

    def clear_rate_limits(self):
        for k in self.keys("rl:*"):
            self.cmd("DEL", k)


redis = Redis(os.environ["REDIS_ADDR"]) if os.environ.get("REDIS_ADDR") else None
JWT_SECRET = os.environ.get("JWT_SECRET", "")


def b64url(data):
    return base64.urlsafe_b64encode(data).rstrip(b"=").decode()


def jwt_claims(token):
    payload = token.split(".")[1]
    return json.loads(base64.urlsafe_b64decode(payload + "=" * (-len(payload) % 4)))


def forge(claims, secret):
    header = b64url(json.dumps({"alg": "HS256", "typ": "JWT"}).encode())
    body = b64url(json.dumps(claims).encode())
    sig = b64url(hmac.new(secret.encode(), f"{header}.{body}".encode(), hashlib.sha256).digest())
    return f"{header}.{body}.{sig}"


# ── people ──────────────────────────────────────────────────────────────────

_used = set()


def new_mobile():
    while True:
        m = str(random.choice([6, 7, 8])) + "".join(random.choice("0123456789") for _ in range(9))
        if m not in _used and not m.startswith(("9000", "9100", "9200", "9300", "9400", "9500")):
            _used.add(m)
            return m


class User:
    """A person signed in through OTP. refresh() picks up new roles."""

    def __init__(self, mobile=None, first=None, last="Tester", activity=None):
        self.mobile = mobile or new_mobile()
        out = self.login()
        self.is_new = out.get("is_new_user", False)
        if first:
            call("PUT", "/users/me", self.token, {"first_name": first, "last_name": last})
        if activity:
            call("POST", "/users/me/onboarding", self.token, {"initial_activity": activity})

    def login(self):
        if redis:
            redis.clear_rate_limits()
        code, _ = call("POST", "/auth/send-otp", body={"mobile": self.mobile})
        require(f"send OTP to {self.mobile}", code == 200, code)
        code, out = call("POST", "/auth/verify-otp", body={"mobile": self.mobile, "otp": OTP, "device_type": "ANDROID", "app_version": "1.0.0"})
        require(f"sign in {self.mobile}", code == 200, out)
        self.token, self.refresh_token = out["access_token"], out["refresh_token"]
        self.id = out["user"]["id"]
        return out

    def refresh(self):
        code, out = call("POST", "/auth/refresh-token", body={"refresh_token": self.refresh_token})
        require("refresh token", code == 200, out)
        self.token, self.refresh_token = out["access_token"], out["refresh_token"]
        return out

    def roles(self):
        _, out = call("GET", "/auth/me", self.token)
        return set(out.get("user", {}).get("roles", []))


def admin():
    return User(ADMIN_MOBILE)


# ═════════════════════════════════════════════════════════════════════════════
# Journeys
# ═════════════════════════════════════════════════════════════════════════════

def journey_auth():
    """Sign-up and login by OTP, sessions, refresh rotation, logout, token expiry and forgery."""
    m = new_mobile()
    if redis:
        redis.clear_rate_limits()

    # Sign-up
    out = expect("send OTP", "POST", "/auth/send-otp", body={"mobile": m})
    check("mock mode returns the dev OTP", out.get("mock_otp") == OTP, out)
    if redis:
        otp_keys = [k for k in redis.keys("*otp*") if m in k and not k.startswith("rl:")]
        check("OTP stored in Redis for this number", len(otp_keys) >= 1, otp_keys)
        stored = redis.cmd("GET", otp_keys[0]) if otp_keys else None
        check("OTP is stored hashed, never as the plain code", stored not in (None, OTP), stored)
        ttl = redis.cmd("TTL", otp_keys[0]) if otp_keys else -2
        check("OTP expires within 5 minutes", 0 < ttl <= 300, ttl)

    out = expect("wrong OTP is refused", "POST", "/auth/verify-otp", body={"mobile": m, "otp": "000000"}, status=401)
    out = expect("first verify signs up", "POST", "/auth/verify-otp", body={"mobile": m, "otp": OTP})
    check("is_new_user on first sign-in", out.get("is_new_user") is True, out.get("is_new_user"))
    check("new user gets the BUYER role", "BUYER" in out["user"].get("roles", []), out["user"].get("roles"))
    access, refresh_tok, uid = out["access_token"], out["refresh_token"], out["user"]["id"]
    claims = jwt_claims(access)
    check("access token lives 15 minutes", 14 * 60 <= claims["exp"] - claims["iat"] <= 15 * 60, claims["exp"] - claims["iat"])
    rclaims = jwt_claims(refresh_tok)
    check("refresh token lives 30 days", 29 * 86400 <= rclaims["exp"] - rclaims["iat"] <= 30 * 86400 + 60, rclaims["exp"] - rclaims["iat"])
    expect("the OTP cannot be replayed after use", "POST", "/auth/verify-otp", body={"mobile": m, "otp": OTP}, status=(200, 401))

    # Same person, different formatting → same account
    if redis:
        redis.clear_rate_limits()
    expect("send OTP with +91 prefix", "POST", "/auth/send-otp", body={"mobile": "+91 " + m})
    out = expect("verify with +91 prefix", "POST", "/auth/verify-otp", body={"mobile": "+91 " + m, "otp": OTP})
    check("+91 / spaced number signs into the SAME account", out.get("user", {}).get("id") == uid, {"first": uid, "second": out.get("user", {}).get("id")})
    check("…and is not a new user", out.get("is_new_user") is False, out.get("is_new_user"))

    # Invalid numbers
    for bad in ["12345", "abcdefghij", "", "0000000000"]:
        code, out = call("POST", "/auth/send-otp", body={"mobile": bad})
        check(f"send OTP refuses invalid mobile {bad!r}", code == 400, {"status": code, "body": out})

    # Me, sessions
    out = expect("GET /auth/me", "GET", "/auth/me", access)
    check("me returns the same user", out.get("user", {}).get("id") == uid, out)
    expect("no token → 401", "GET", "/auth/me", status=401)
    expect("garbage token → 401", "GET", "/auth/me", "not-a-jwt", status=401)

    # Refresh rotation
    out = expect("refresh token", "POST", "/auth/refresh-token", body={"refresh_token": refresh_tok})
    new_access, new_refresh = out.get("access_token"), out.get("refresh_token")
    check("refresh issues a new refresh token", new_refresh and new_refresh != refresh_tok)
    expect("the old refresh token is dead after rotation", "POST", "/auth/refresh-token", body={"refresh_token": refresh_tok}, status=401)
    expect("an access token is not accepted as a refresh token", "POST", "/auth/refresh-token", body={"refresh_token": new_access}, status=401)

    # Forged and expired tokens
    if JWT_SECRET:
        c = jwt_claims(new_access)
        expired = dict(c, iat=c["iat"] - 7200, exp=int(time.time()) - 60)
        expect("expired access token → 401", "GET", "/auth/me", forge(expired, JWT_SECRET), status=401)
        expect("token signed with another secret → 401", "GET", "/auth/me", forge(c, "attacker-secret"), status=401)
        elevated = dict(c, roles=["ADMIN", "SUPER_ADMIN"])
        code, _ = call("GET", "/admin/dashboard", forge(elevated, "attacker-secret"))
        check("forged ADMIN role with a wrong key is refused", code == 401, code)
        none_alg = b64url(json.dumps({"alg": "none", "typ": "JWT"}).encode()) + "." + b64url(json.dumps(elevated).encode()) + "."
        expect("alg=none token → 401", "GET", "/auth/me", none_alg, status=401)

    # Logout
    expect("logout", "POST", "/auth/logout", new_access, {"refresh_token": new_refresh})
    expect("access token is revoked after logout", "GET", "/auth/me", new_access, status=401)
    expect("refresh token is dead after logout", "POST", "/auth/refresh-token", body={"refresh_token": new_refresh}, status=401)
    if redis:
        check("revoked token is in the Redis blocklist", any(k.startswith("blocklist:") for k in redis.keys("blocklist:*")))

    # Rate limits
    if redis:
        redis.clear_rate_limits()
        flood = new_mobile()
        codes = [call("POST", "/auth/send-otp", body={"mobile": flood})[0] for _ in range(7)]
        check("send OTP: 6th request in 10 min is rate-limited (429)", codes[:5] == [200] * 5 and 429 in codes[5:], codes)
        redis.clear_rate_limits()
        bad_codes = [call("POST", "/auth/verify-otp", body={"mobile": flood, "otp": "999999"})[0] for _ in range(11)]
        check("verify OTP: 11th attempt from one IP in 15 min is rate-limited", 429 in bad_codes[-1:], bad_codes)
        redis.clear_rate_limits()


def journey_accounts():
    """Profile and onboarding, admin-only areas, suspension and reinstatement, account deletion."""
    boss = admin()
    u = User(first="Asha", activity="CUSTOMER")
    me = expect("read own profile", "GET", "/users/me", u.token)
    check("profile saved", me.get("user", {}).get("first_name") == "Asha", me.get("user"))
    out = expect("own app rating before rating", "GET", "/ratings/app", u.token)
    check("…none yet", out.get("rating") is None, out)
    expect("rate the app", "POST", "/ratings/app", u.token, {"overall_rating": 4, "would_recommend": True, "comment": "Easy"}, status=(200, 201))
    out = expect("own app rating after rating (the form shows it)", "GET", "/ratings/app", u.token)
    check("…4 stars, recommended, comment kept", (out.get("rating") or {}).get("overall_rating") == 4 and out["rating"].get("would_recommend") is True and out["rating"].get("comment") == "Easy", out)
    expect("invalid onboarding activity refused", "POST", "/users/me/onboarding", u.token, {"initial_activity": "ROCKET"}, status=400)

    # Change the sign-in number: code to the NEW number, then switch
    other = User(first="Other", activity="CUSTOMER")
    new_mobile_no = new_mobile()
    expect("change number: invalid number refused", "POST", "/users/me/mobile/send-code", u.token, {"mobile": "12345"}, status=400)
    expect("change number: own number refused", "POST", "/users/me/mobile/send-code", u.token, {"mobile": u.mobile}, status=400)
    expect("change number: someone else's number refused", "POST", "/users/me/mobile/send-code", u.token, {"mobile": other.mobile}, status=409)
    out = expect("change number: code sent to the new number", "POST", "/users/me/mobile/send-code", u.token, {"mobile": "+91 " + new_mobile_no})
    check("…delivered to the normalized new number", out.get("delivered_to") == new_mobile_no, out)
    expect("change number: wrong code refused", "POST", "/users/me/mobile/verify", u.token, {"mobile": new_mobile_no, "code": "000000"}, status=400)
    out = expect("change number: verified", "POST", "/users/me/mobile/verify", u.token, {"mobile": new_mobile_no, "code": OTP})
    check("…profile shows the new number", out["user"]["mobile"] == new_mobile_no, out["user"])
    old_mobile, u.mobile = u.mobile, new_mobile_no
    again = u.login()
    check("sign in with the new number reaches the same account", again["user"]["id"] == u.id, again["user"])
    fresh = User(old_mobile)
    check("the old number is free (signs up a new account)", fresh.id != u.id and fresh.is_new, {"id": fresh.id, "is_new": fresh.is_new})
    addr = expect("add a delivery address", "POST", f"/users/{u.id}/addresses", u.token, {
        "address_type": "DELIVERY", "contact_name": "Asha", "contact_mobile": u.mobile, "address_line1": "12 MG Road",
        "city": "Hyderabad", "state": "Telangana", "country": "India", "postal_code": "500001"}, status=(200, 201))
    check("…the first address is the default", addr["address"]["is_default"] is True, addr)
    second = expect("add a second address", "POST", f"/users/{u.id}/addresses", u.token, {
        "address_line1": "4 Park Lane", "city": "Hyderabad", "country": "India"}, status=(200, 201))
    check("…not the default", second["address"]["is_default"] is False, second)
    expect("delete the default address", "DELETE", f"/users/addresses/{addr['address']['id']}", u.token, status=(200, 204))
    out = expect("list addresses", "GET", f"/users/{u.id}/addresses", u.token)
    check("…the remaining address became the default", [a["is_default"] for a in out["addresses"]] == [True], out)
    other = User(first="Mallik", activity="CUSTOMER")
    expect("cannot add an address to someone else's account", "POST", f"/users/{u.id}/addresses", other.token, {
        "address_line1": "x", "city": "y", "country": "India"}, status=(403, 404))
    expect("cannot read someone else's profile by id", "GET", f"/users/{u.id}", other.token, status=(403, 404))

    # Admin-only areas
    for path in ["/admin/dashboard", "/admin/users", "/audit-logs", "/security-logs", "/botanical-names", "/market/reports", "/admin/crash-reports"]:
        expect(f"buyer refused on {path}", "GET", path, u.token, status=403)
        expect(f"admin allowed on {path}", "GET", path, boss.token, status=200)

    # Suspension
    v = User(first="Ravi", activity="CUSTOMER")
    expect("admin suspends a user", "PUT", f"/admin/users/{v.id}/status", boss.token, {"status": "SUSPENDED", "reason": "fraud check"})
    out = expect("suspended user's current token is refused", "GET", "/users/me", v.token, status=403)
    check("…with code USER_SUSPENDED", err_code(out) == "USER_SUSPENDED", out)
    if redis:
        check("suspension is recorded in Redis (survives API restarts)", bool(redis.keys(f"suspension:user:{v.id}")))
    if redis:
        redis.clear_rate_limits()
    call("POST", "/auth/send-otp", body={"mobile": v.mobile})
    out = expect("suspended user cannot sign in again", "POST", "/auth/verify-otp", body={"mobile": v.mobile, "otp": OTP}, status=403)
    code, out = call("POST", "/auth/refresh-token", body={"refresh_token": v.refresh_token})
    check("suspended user cannot refresh a token", code in (401, 403), {"status": code, "body": out})
    expect("user cannot suspend themselves via admin API", "PUT", f"/admin/users/{u.id}/status", u.token, {"status": "SUSPENDED"}, status=403)
    expect("admin reinstates the user", "PUT", f"/admin/users/{v.id}/status", boss.token, {"status": "ACTIVE", "reason": "cleared"})
    if redis:
        redis.clear_rate_limits()
    call("POST", "/auth/send-otp", body={"mobile": v.mobile})
    out = expect("reinstated user signs in", "POST", "/auth/verify-otp", body={"mobile": v.mobile, "otp": OTP})
    expect("…and can use the API", "GET", "/users/me", out.get("access_token"))

    # Deletion
    w = User(first="Delete", last="Me", activity="CUSTOMER")
    expect("buyer deletes own account", "DELETE", "/users/me", w.token, status=(200, 204))
    expect("deleted account's token stops working", "GET", "/users/me", w.token, status=(401, 403))
    expect("deleted account's refresh token stops working", "POST", "/auth/refresh-token", body={"refresh_token": w.refresh_token}, status=(401, 403))
    again = User(w.mobile)
    check("the same number signs up again as a NEW account", again.id != w.id and again.is_new, {"old": w.id, "new": again.id})


# ── shared set-ups for the trade journeys ──────────────────────────────────

# Journey nurseries sit in Port Blair, over 1,000 km from the dev users in
# Hyderabad, so the market's 100 km limit keeps test ads out of their lists.
PORT_BLAIR = (11.6234, 92.7265)


def approved_owner(boss, name="Journey Nursery", selfie=True, coords=True):
    """A nursery owner whose nursery the admin has approved (trial subscription), with a profile selfie."""
    owner = User(first="Owner", activity="OWNER")
    body = {"name": f"{name} {owner.mobile[-4:]}", "mobile": owner.mobile, "address_line1": "Plot 1",
            "city": "Hyderabad", "state": "Telangana", "country": "India", "postal_code": "500032"}
    if coords:
        body.update(latitude=PORT_BLAIR[0], longitude=PORT_BLAIR[1])
    n = expect("register nursery", "POST", "/nurseries", owner.token, body, status=201)
    nid = n["nursery"]["id"]
    expect("admin approves nursery", "PUT", f"/nurseries/{nid}/status", boss.token, {"status": "APPROVED"})
    owner.refresh()
    owner.nursery_id = nid
    if selfie:
        upload_selfie("owner takes a profile selfie", owner)
    return owner


def invite(owner, kind, person):
    out = expect(f"owner sends {kind}", "POST", "/invites", owner.token,
                 {"invite_type": kind, "nursery_id": owner.nursery_id, "target_mobile": person.mobile, "target_name": "Journey"}, status=(200, 201))
    return out["invite"]["invite_uuid"]


def approved_driver(boss, owner):
    d = User(first="Driver", activity="DRIVER")
    uuid = invite(owner, "DRIVER_INVITE", d)
    expect("driver accepts invite", "POST", f"/invites/{uuid}/accept", d.token, {})
    app = expect("driver applies with licence", "POST", "/drivers/apply", d.token,
                 {"licence_number": "TS09" + d.mobile, "vehicle_number": "TS09" + d.mobile[-4:], "vehicle_type": "MINI_TRUCK"}, status=(200, 201))
    expect("admin approves driver", "POST", f"/drivers/{app['driver']['id']}/approve", boss.token, {"driver_user_id": d.id})
    d.refresh()
    return d


def team(boss):
    """Approved owner + manager + buyer + approved driver + two plants listed as top items."""
    owner = approved_owner(boss)
    manager = User(first="Manager", activity="MANAGER")
    expect("manager accepts invite", "POST", f"/invites/{invite(owner, 'MANAGER_INVITE', manager)}/accept", manager.token, {})
    manager.refresh()
    upload_selfie("manager takes a profile selfie", manager)
    buyer = User(first="Buyer", activity="CUSTOMER")
    driver = approved_driver(boss, owner)
    _, sizes = call("GET", "/plants/sizes")
    size = sizes["sizes"][1]["id"]
    plants = []
    # Mango and neem are in seed-master, so this works without the botanical-names load (CI).
    for wfo, name in [("wfo-0000371248", "Mangifera indica"), ("wfo-0000557668", "Azadirachta indica")]:
        code, body = call("POST", "/plants/resolve", owner.token, {"wfo_id": wfo})
        if code != 200:
            _, found = call("GET", f"/plants?search={urllib.parse.quote(name)}", owner.token)
            body = {"plant": found["plants"][0]}
        plants.append(body["plant"]["id"])
        expect("list plant as a top item", "POST", "/top-items", owner.token,
               {"nursery_id": owner.nursery_id, "plant_id": body["plant"]["id"], "size_id": size, "unit_price": 150}, status=(201, 409))
    return owner, manager, buyer, driver, plants


def journey_nursery():
    """Nursery register → reject → resubmit → approve → suspend → reactivate; one nursery per owner; invites and role rules; subscription gate."""
    boss = admin()
    owner = User(first="Priya", activity="OWNER")
    body = {"name": f"Priya Greens {owner.mobile[-4:]}", "mobile": owner.mobile, "address_line1": "Plot 9", "city": "Hyderabad", "state": "Telangana", "country": "India"}
    n = expect("owner registers a nursery", "POST", "/nurseries", owner.token, body, status=201)
    nid = n["nursery"]["id"]
    check("new nursery is PENDING", n["nursery"]["status"] == "PENDING", n["nursery"]["status"])
    expect("a second nursery for the same owner is refused", "POST", "/nurseries", owner.token, dict(body, name="Second"), status=409)
    expect("pending nursery cannot list top items", "POST", "/top-items", owner.token,
           {"nursery_id": nid, "plant_id": 1, "size_id": 2}, status=(402, 403))
    expect("pending nursery cannot create a quotation", "POST", "/quotations", owner.token,
           {"quotation_type": "CUSTOMER", "nursery_id": nid, "recipient_name": "x", "recipient_mobile": "9876543210", "items": [{"plant_id": 1, "quantity": 1, "unit_price": 1, "total_price": 1}]}, status=(402, 403))
    expect("owner cannot approve their own nursery", "PUT", f"/nurseries/{nid}/status", owner.token, {"status": "APPROVED"}, status=403)
    expect("reject without a reason is refused", "PUT", f"/nurseries/{nid}/status", boss.token, {"status": "REJECTED"}, status=400)
    expect("suspend is not a review decision", "PUT", f"/nurseries/{nid}/status", boss.token, {"status": "SUSPENDED"}, status=400)
    expect("admin rejects with a reason", "PUT", f"/nurseries/{nid}/status", boss.token, {"status": "REJECTED", "reason": "Blurry licence photo"})
    expect("a rejected application cannot be approved without resubmitting", "PUT", f"/nurseries/{nid}/status", boss.token, {"status": "APPROVED"}, status=409)
    out = expect("owner sees the rejection", "GET", "/nurseries/owned", owner.token)
    check("status REJECTED with reason", "REJECTED" in json.dumps(out) and "Blurry" in json.dumps(out), out)
    titles = []
    for _ in range(20):  # the notification worker is asynchronous
        titles = [n.get("title") for n in call("GET", "/notifications", owner.token)[1].get("notifications", [])]
        if "Nursery application not approved" in titles:
            break
        time.sleep(0.25)
    check("…and is notified, with the reason", "Nursery application not approved" in titles, titles)
    expect("resubmit without details is refused", "POST", "/nurseries/owned/resubmit", owner.token, {}, status=400)
    expect("owner resubmits", "POST", "/nurseries/owned/resubmit", owner.token, dict(body, name=body["name"] + " (clear photo)"), status=(200, 201))
    expect("a pending application cannot be resubmitted", "POST", "/nurseries/owned/resubmit", owner.token, body, status=409)
    out = expect("resubmitted nursery is PENDING again", "GET", "/nurseries/owned", owner.token)
    check("…PENDING", '"PENDING"' in json.dumps(out), out)
    expect("admin approves", "PUT", f"/nurseries/{nid}/status", boss.token, {"status": "APPROVED"})
    owner.refresh()
    check("owner now has NURSERY_OWNER", "NURSERY_OWNER" in owner.roles(), owner.roles())
    sub = expect("approval created a trial subscription", "GET", "/subscriptions/me", owner.token)
    sub_json = json.dumps(sub).upper()
    check("…TRIAL and active", "TRIAL" in sub_json and "ACTIVE" in sub_json, sub)
    trial = (sub.get("subscriptions") or [{}])[0]
    days = (datetime.date.fromisoformat(trial.get("end_date", "")[:10]) - datetime.date.fromisoformat(trial.get("start_date", "")[:10])).days + 1 if trial.get("end_date") else 0
    check("…a one-month free trial", 28 <= days <= 31, (trial.get("start_date"), trial.get("end_date")))
    owner.nursery_id = nid
    expect("approving twice is refused", "PUT", f"/nurseries/{nid}/status", boss.token, {"status": "APPROVED"}, status=409)
    expect("owner edits their nursery", "PUT", f"/nurseries/{nid}", owner.token, {"description": "Fruit saplings"})
    out = expect("owner cannot change status through the edit form", "PUT", f"/nurseries/{nid}", owner.token, {"status": "SUSPENDED"})
    check("…status unchanged", out["nursery"]["status"] == "APPROVED", out["nursery"]["status"])
    # Orders pick up from the primary address, so a nursery always keeps one.
    out = expect("owner lists nursery addresses", "GET", f"/nurseries/{nid}/addresses", owner.token)
    first = out["addresses"][0]
    check("…the registration address is primary", first["is_primary"] is True, out)
    expect("the only nursery address cannot be deleted", "DELETE", f"/nurseries/addresses/{first['id']}", owner.token, status=409)
    expect("owner adds a second nursery address", "POST", f"/nurseries/{nid}/addresses", owner.token,
           {"address_line1": "Godown 2", "city": "Hyderabad", "country": "India"}, status=(200, 201))
    expect("owner deletes the primary address", "DELETE", f"/nurseries/addresses/{first['id']}", owner.token, status=(200, 204))
    out = expect("owner lists nursery addresses again", "GET", f"/nurseries/{nid}/addresses", owner.token)
    check("…the remaining address became primary", [a["is_primary"] for a in out["addresses"]] == [True], out)

    # Who may register a nursery
    d_boss = approved_owner(boss, "Driver Boss")
    driver = approved_driver(boss, d_boss)
    other = d_boss.nursery_id
    expect("an owner cannot edit another owner's nursery", "PUT", f"/nurseries/{other}", owner.token, {"name": "Hijacked"}, status=403)
    expect("an owner cannot delete another owner's nursery", "DELETE", f"/nurseries/{other}", owner.token, status=403)
    expect("an owner cannot list another nursery's users", "GET", f"/nurseries/{other}/users", owner.token, status=403)
    expect("an owner cannot add an address to another nursery", "POST", f"/nurseries/{other}/addresses", owner.token, {"address_line1": "x", "city": "y"}, status=403)
    expect("a driver cannot register a nursery", "POST", "/nurseries", driver.token, dict(body, name="Driver nursery"), status=(403, 409))

    # Invites and role exclusivity
    manager = User(first="Gumastha", activity="MANAGER")
    stranger = User(first="Stranger", activity="CUSTOMER")
    uuid = invite(owner, "MANAGER_INVITE", manager)
    expect("someone else cannot accept an invite meant for the manager", "POST", f"/invites/{uuid}/accept", stranger.token, {}, status=403)
    expect("buyer cannot send invites", "POST", "/invites", stranger.token, {"invite_type": "MANAGER_INVITE", "nursery_id": nid, "target_mobile": manager.mobile}, status=403)
    expect("invite to an invalid mobile refused", "POST", "/invites", owner.token, {"invite_type": "MANAGER_INVITE", "nursery_id": nid, "target_mobile": "12345"}, status=400)
    expect("manager accepts", "POST", f"/invites/{uuid}/accept", manager.token, {})
    expect("an accepted invite cannot be accepted again", "POST", f"/invites/{uuid}/accept", manager.token, {}, status=(409, 400, 410))
    manager.refresh()
    roles = manager.roles()
    check("manager gains MANAGER and loses BUYER", "MANAGER" in roles and "BUYER" not in roles, roles)
    expect("a manager cannot register a nursery", "POST", "/nurseries", manager.token, dict(body, name="Manager nursery"), status=409)
    out = expect("owner sees the manager", "GET", f"/nurseries/{nid}/managers", owner.token)
    check("…listed", manager.mobile in json.dumps(out) or str(manager.id) in json.dumps(out), out)
    check("driver lost BUYER when approved", "BUYER" not in driver.roles(), driver.roles())

    cancelled = User(first="Late", activity="CUSTOMER")
    uuid2 = invite(owner, "CUSTOMER_INVITE", cancelled)
    expect("owner cancels an invite", "POST", f"/invites/{uuid2}/cancel", owner.token, {})
    expect("a cancelled invite cannot be accepted", "POST", f"/invites/{uuid2}/accept", cancelled.token, {}, status=(400, 404, 409, 410))
    customer = User(first="Customer", activity="CUSTOMER")
    expect("customer accepts customer invite", "POST", f"/invites/{invite(owner, 'CUSTOMER_INVITE', customer)}/accept", customer.token, {})
    out = expect("owner sees the customer", "GET", f"/nurseries/{nid}/customers", owner.token)
    check("…listed", customer.mobile in json.dumps(out) or str(customer.id) in json.dumps(out), out)
    expect("a driver cannot become a customer", "POST", f"/invites/{invite(owner, 'CUSTOMER_INVITE', driver)}/accept", driver.token, {}, status=(403, 409))

    # Manager leaves
    expect("manager leaves the nursery", "DELETE", "/nurseries/me/leave", manager.token, status=(200, 204))
    code, _ = call("POST", "/auth/refresh-token", body={"refresh_token": manager.refresh_token})
    check("leaving ends the manager's sessions", code == 401, code)
    manager.login()
    roles = manager.roles()
    check("…and is a BUYER again, not a MANAGER", "MANAGER" not in roles and "BUYER" in roles, roles)
    uuid3 = invite(owner, "MANAGER_INVITE", manager)
    expect("a former manager can rejoin by a new invite", "POST", f"/invites/{uuid3}/accept", manager.token, {})
    manager.refresh()
    check("…MANAGER again", "MANAGER" in manager.roles(), manager.roles())
    expect("owner removes the manager", "DELETE", f"/nurseries/{nid}/managers/{manager.id}", owner.token, status=(200, 204))
    expect("removing twice is not found", "DELETE", f"/nurseries/{nid}/managers/{manager.id}", owner.token, status=404)

    # Suspension of the nursery and the subscription gate
    expect("admin suspends the nursery", "PUT", f"/admin/nurseries/{nid}/status", boss.token, {"status": "SUSPENDED", "reason": "complaints"})
    q = {"quotation_type": "CUSTOMER", "nursery_id": nid, "recipient_name": "Ravi", "recipient_mobile": customer.mobile,
         "items": [{"plant_id": 1, "quantity": 1, "unit_price": 100, "total_price": 100}]}
    expect("suspended nursery cannot create a quotation", "POST", "/quotations", owner.token, q, status=(402, 403))
    expect("admin reactivates the nursery", "PUT", f"/admin/nurseries/{nid}/status", boss.token, {"status": "ACTIVE", "reason": "resolved"})
    owner.login()
    expect("reactivated nursery can create a quotation", "POST", "/quotations", owner.token, q, status=201)
    sub_id = sub["subscriptions"][0]["id"]
    expect("admin cancels the subscription", "POST", f"/subscriptions/{sub_id}/cancel", boss.token, {"cancel_immediately": True, "reason": "journey"})
    owner.refresh()
    out = expect("without a subscription, quotations are refused", "POST", "/quotations", owner.token, q, status=402)
    check("…code subscription_required", err_code(out) == "subscription_required", out)


def journey_quotation():
    """Quotation lifecycle: draft → send → recall → send → accept → convert; reject; expiry; manager privacy and exclusive editor; deletes."""
    boss = admin()
    owner, manager, buyer, _, plants = team(boss)
    rival = approved_owner(boss, "Rival")
    nid = owner.nursery_id
    mango, jamun = plants

    def quote(**kw):
        q = {"quotation_type": "CUSTOMER", "nursery_id": nid, "recipient_name": "Ravi", "recipient_mobile": buyer.mobile,
             "items": [{"plant_id": mango, "quantity": 10, "unit_price": 150, "total_price": 1500}]}
        q.update(kw)
        return q

    # Validation
    expect("buyer cannot create a quotation", "POST", "/quotations", buyer.token, quote(), status=403)
    expect("quotation without items is refused", "POST", "/quotations", owner.token, quote(items=[]), status=400)
    expect("zero quantity is refused", "POST", "/quotations", owner.token, quote(items=[{"plant_id": mango, "quantity": 0, "unit_price": 1, "total_price": 0}]), status=400)
    expect("negative price is refused", "POST", "/quotations", owner.token, quote(items=[{"plant_id": mango, "quantity": 1, "unit_price": -5, "total_price": -5}]), status=400)
    expect("unknown plant is refused", "POST", "/quotations", owner.token, quote(items=[{"plant_id": 999999999, "quantity": 1, "unit_price": 1, "total_price": 1}]), status=(400, 404))
    expect("customer quotation needs a recipient", "POST", "/quotations", owner.token, quote(recipient_name=None, recipient_mobile=None), status=400)
    expect("invalid recipient mobile is refused", "POST", "/quotations", owner.token, quote(recipient_mobile="12345"), status=400)
    expect("unknown quotation type is refused", "POST", "/quotations", owner.token, quote(quotation_type="GIFT"), status=400)
    expect("owner cannot quote for another nursery", "POST", "/quotations", owner.token, quote(nursery_id=rival.nursery_id), status=403)
    expect("manager cannot pre-assign", "POST", "/quotations", manager.token, quote(assigned_manager_user_id=manager.id), status=403)
    expect("pre-assign to a non-member is refused", "POST", "/quotations", owner.token, quote(assigned_manager_user_id=buyer.id), status=(400, 403, 404))
    pre = expect("owner creates a quotation already assigned to the manager", "POST", "/quotations", owner.token, quote(assigned_manager_user_id=manager.id), status=201)["quotation"]
    titles = []
    for _ in range(20):
        titles = [n.get("message", "") for n in call("GET", "/notifications", manager.token)[1].get("notifications", [])]
        if any(pre["quotation_code"] in t for t in titles):
            break
        time.sleep(0.25)
    check("…the manager is notified, as with Assign Manager", any(pre["quotation_code"] in t for t in titles), titles)
    expect("a line total that is not quantity × price is refused", "POST", "/quotations", owner.token,
           quote(items=[{"plant_id": mango, "quantity": 3, "unit_price": 100, "total_price": 1}]), status=400)

    # Internal quotation
    out = expect("owner creates an internal quotation", "POST", "/quotations", owner.token,
                 {"quotation_type": "INTERNAL", "nursery_id": nid, "items": [{"plant_id": jamun, "quantity": 5, "unit_price": 80, "total_price": 400}]}, status=201)
    internal = out["quotation"]["id"]
    check("…INTERNAL_DRAFT", out["quotation"]["status"] == "INTERNAL_DRAFT", out["quotation"]["status"])
    expect("an internal quotation cannot be sent", "POST", f"/quotations/{internal}/send", owner.token, {}, status=(400, 409))

    # Customer quotation by the owner
    q1 = expect("owner creates a customer quotation", "POST", "/quotations", owner.token, quote(), status=201)["quotation"]
    qid = q1["id"]
    check("…CUSTOMER_DRAFT, code issued", q1["status"] == "CUSTOMER_DRAFT" and q1.get("quotation_code", "").startswith("QUO-"), q1)
    expect("buyer cannot see a draft", "GET", f"/quotations/{qid}", buyer.token, status=(403, 404))
    out = expect("buyer's list has no drafts", "GET", "/quotations?buying=true", buyer.token)
    check("…not listed", qid not in [x["id"] for x in out.get("quotations", [])], out)
    expect("buyer cannot accept a draft", "POST", f"/quotations/{qid}/buyer-accept", buyer.token, {}, status=(403, 404, 409))
    expect("a rival owner cannot see it", "GET", f"/quotations/{qid}", rival.token, status=(403, 404))
    expect("a rival owner cannot edit it", "PUT", f"/quotations/{qid}", rival.token, quote(notes="x"), status=(403, 404))
    expect("a rival owner cannot delete it", "DELETE", f"/quotations/{qid}", rival.token, status=(403, 404))

    # Manager privacy and the exclusive editor
    expect("manager cannot see the owner's unassigned quotation", "GET", f"/quotations/{qid}", manager.token, status=(403, 404))
    out = expect("manager list", "GET", "/quotations", manager.token)
    check("…does not include it", qid not in [x["id"] for x in out.get("quotations", [])], out)
    expect("manager cannot assign", "POST", f"/quotations/{qid}/assign-manager", manager.token, {"manager_user_id": manager.id}, status=403)
    expect("owner cannot assign a non-member", "POST", f"/quotations/{qid}/assign-manager", owner.token, {"manager_user_id": buyer.id}, status=(400, 403, 404))
    expect("owner assigns the manager", "POST", f"/quotations/{qid}/assign-manager", owner.token, {"manager_user_id": manager.id})
    mq = expect("assigned manager sees it", "GET", f"/quotations/{qid}", manager.token)["quotation"]
    check("…recipient hidden from the manager", not mq.get("recipient_mobile") and not mq.get("recipient_name"), mq)
    expect("owner cannot edit while a manager is assigned", "PUT", f"/quotations/{qid}", owner.token, quote(notes="owner edit"), status=403)
    oq = expect("owner opens the assigned quotation", "GET", f"/quotations/{qid}", owner.token)["quotation"]
    check("…its capabilities say the owner cannot edit (no pencil that then fails)", oq["capabilities"]["can_edit"] is False, oq["capabilities"])
    check("…the assigned manager can", mq["capabilities"]["can_edit"] is True, mq["capabilities"])
    out = expect("assigned manager edits", "PUT", f"/quotations/{qid}", manager.token,
                 quote(items=[{"plant_id": mango, "quantity": 20, "unit_price": 150, "total_price": 3000}, {"plant_id": jamun, "quantity": 5, "unit_price": 80, "total_price": 400}]))
    check("…total recalculated to 3400", float(out["quotation"]["total_amount"]) == 3400, out["quotation"]["total_amount"])
    expect("owner unassigns", "DELETE", f"/quotations/{qid}/assign-manager", owner.token)
    expect("manager loses sight of it", "GET", f"/quotations/{qid}", manager.token, status=(403, 404))
    expect("owner can edit again", "PUT", f"/quotations/{qid}", owner.token, quote(notes="final", items=[{"plant_id": mango, "quantity": 20, "unit_price": 150, "total_price": 3000}]))

    # Send, recall, send, accept, convert
    out = expect("owner sends", "POST", f"/quotations/{qid}/send", owner.token, {})
    check("…CUSTOMER_SENT with valid_until", out["quotation"]["status"] == "CUSTOMER_SENT" and out["quotation"].get("valid_until"), out["quotation"])
    bq = expect("buyer opens the sent quotation", "GET", f"/quotations/{qid}", buyer.token)["quotation"]
    check("…internal notes and handler are not shown to the buyer", not bq.get("notes") and not bq.get("assigned_manager_name"), bq)
    oq = expect("owner opens it", "GET", f"/quotations/{qid}", owner.token)["quotation"]
    check("…the owner still sees the notes", oq.get("notes") == "final", oq.get("notes"))
    expect("sending twice is refused", "POST", f"/quotations/{qid}/send", owner.token, {}, status=(400, 409))
    expect("a sent quotation cannot be edited", "PUT", f"/quotations/{qid}", owner.token, quote(notes="late"), status=(400, 409))
    expect("a sent quotation cannot be converted", "POST", f"/quotations/{qid}/convert-to-order", owner.token, {}, status=(400, 409))
    out = expect("buyer sees it now", "GET", "/quotations?buying=true", buyer.token)
    check("…listed for the buyer", qid in [x["id"] for x in out.get("quotations", [])], out)
    expect("owner recalls", "POST", f"/quotations/{qid}/recall", owner.token, {})
    expect("recalled quotation is hidden from the buyer", "GET", f"/quotations/{qid}", buyer.token, status=(403, 404))
    expect("owner sends again", "POST", f"/quotations/{qid}/send", owner.token, {})
    stranger = User(first="Nosy", activity="CUSTOMER")
    expect("another buyer cannot accept it", "POST", f"/quotations/{qid}/buyer-accept", stranger.token, {}, status=(403, 404))
    expect("owner cannot accept on the buyer's behalf", "POST", f"/quotations/{qid}/buyer-accept", owner.token, {}, status=(403, 404))
    # A connected customer: only the buyer's own action connects them, never
    # the nursery typing their number.
    out = expect("customers before the buyer answers", "GET", f"/nurseries/{nid}/customers", owner.token)
    check("…a buyer who was only sent a quotation is not a customer yet", buyer.id not in [c["user_id"] for c in out["customers"]], out)
    expect("…so the owner cannot pick them by account", "POST", "/quotations", owner.token, quote(customer_user_id=buyer.id), status=403)
    out = expect("buyer accepts", "POST", f"/quotations/{qid}/buyer-accept", buyer.token, {})
    check("…CUSTOMER_ACCEPTED", out["quotation"]["status"] == "CUSTOMER_ACCEPTED", out["quotation"]["status"])
    out = expect("customers after the buyer accepted", "GET", f"/nurseries/{nid}/customers", owner.token)
    check("…the buyer is a connected customer now, no invite needed", buyer.id in [c["user_id"] for c in out["customers"]], out)
    check("…a stranger is not", stranger.id not in [c["user_id"] for c in out["customers"]], out)
    expect("owner quotes the connected customer by account", "POST", "/quotations", owner.token, quote(customer_user_id=buyer.id), status=201)
    expect("owner cannot quote a stranger by account", "POST", "/quotations", owner.token, quote(customer_user_id=stranger.id), status=403)
    expect("a rival nursery does not get the customer", "POST", "/quotations", rival.token,
           quote(nursery_id=rival.nursery_id, customer_user_id=buyer.id), status=403)
    expect("a rival cannot list this nursery's customers", "GET", f"/nurseries/{nid}/customers", rival.token, status=403)
    expect("accepting twice is refused", "POST", f"/quotations/{qid}/buyer-accept", buyer.token, {}, status=(400, 409))
    expect("buyer cannot reject after accepting", "POST", f"/quotations/{qid}/buyer-reject", buyer.token, {"reason": "changed mind"}, status=(400, 409))
    expect("an accepted quotation cannot be recalled", "POST", f"/quotations/{qid}/recall", owner.token, {}, status=(400, 409))
    expect("an accepted quotation cannot be deleted", "DELETE", f"/quotations/{qid}", owner.token, status=(400, 409))
    expect("buyer cannot convert", "POST", f"/quotations/{qid}/convert-to-order", buyer.token, {}, status=(403, 404))
    out = expect("owner converts to an order", "POST", f"/quotations/{qid}/convert-to-order", owner.token, {})
    cq = out["quotation"]
    check("…CONVERTED with the order linked", cq["status"] == "CONVERTED" and cq.get("converted_order_id"), cq)
    expect("converting twice is refused", "POST", f"/quotations/{qid}/convert-to-order", owner.token, {}, status=(400, 409))
    for what, method, path, body in [("edit", "PUT", f"/quotations/{qid}", quote()), ("delete", "DELETE", f"/quotations/{qid}", None),
                                     ("assign", "POST", f"/quotations/{qid}/assign-manager", {"manager_user_id": manager.id}),
                                     ("unassign", "DELETE", f"/quotations/{qid}/assign-manager", None)]:
        out = expect(f"converted quotation is locked: {what}", method, path, owner.token, body, status=409)
    order = expect("the new order exists", "GET", f"/orders/{cq['converted_order_id']}", owner.token)
    oj = json.dumps(order)
    check("…for the buyer, 20 mangoes, 3000", str(buyer.id) in oj and "3000" in oj, order)
    bo = expect("buyer sees the order", "GET", f"/orders/{cq['converted_order_id']}", buyer.token)
    bo = bo.get("order", bo)
    check("…without the nursery's internal notes", not bo.get("notes") and not bo.get("assigned_manager_name"), bo.get("notes"))
    out = expect("buyer searches their orders for a word in the internal notes", "GET", "/orders?buying=true&search=final", buyer.token)
    check("…the notes are not searchable by the buyer", cq["converted_order_id"] not in [x["id"] for x in out.get("orders", [])], out.get("orders"))
    out = expect("owner searches the same word", "GET", "/orders?search=final", owner.token)
    check("…the seller still finds it by its notes", cq["converted_order_id"] in [x["id"] for x in out.get("orders", [])], out.get("pagination"))

    # Manager's own quotation, buyer rejection
    q2 = expect("manager creates a quotation", "POST", "/quotations", manager.token, quote(), status=201)["quotation"]
    expect("owner sees the manager's quotation", "GET", f"/quotations/{q2['id']}", owner.token)
    expect("manager cannot delete", "DELETE", f"/quotations/{q2['id']}", manager.token, status=403)
    out = expect("manager sends", "POST", f"/quotations/{q2['id']}/send", manager.token, {})
    check("…send response hides the customer from the manager", not out["quotation"].get("recipient_mobile"), out["quotation"])
    out = expect("buyer rejects with a reason", "POST", f"/quotations/{q2['id']}/buyer-reject", buyer.token, {"reason": "Too costly"})
    check("…CUSTOMER_REJECTED", out["quotation"]["status"] == "CUSTOMER_REJECTED", out["quotation"]["status"])
    oq = expect("owner sees the reason", "GET", f"/quotations/{q2['id']}", owner.token)["quotation"]
    check("…rejection_reason stored, notes untouched", oq.get("rejection_reason") == "Too costly" and "Too costly" not in (oq.get("notes") or ""), oq)
    mq = expect("manager views the rejected quotation", "GET", f"/quotations/{q2['id']}", manager.token)["quotation"]
    check("…reason hidden from the manager", not mq.get("rejection_reason"), mq)
    expect("a rejected quotation cannot be accepted", "POST", f"/quotations/{q2['id']}/buyer-accept", buyer.token, {}, status=(400, 409))
    expect("owner deletes the rejected quotation", "DELETE", f"/quotations/{q2['id']}", owner.token, status=(200, 204))
    expect("a deleted quotation is gone", "GET", f"/quotations/{q2['id']}", owner.token, status=404)

    # Expiry (accepting an expired quotation is covered by unit tests)
    past = "2020-01-01T00:00:00Z"
    expect("a quotation valid until the past is refused", "POST", "/quotations", owner.token, quote(valid_until=past), status=400)
    out = expect("valid_until in the future is kept", "POST", "/quotations", owner.token, quote(valid_until="2099-01-01T00:00:00Z"), status=201)
    expect("editing valid_until into the past is refused", "PUT", f"/quotations/{out['quotation']['id']}", owner.token, quote(valid_until=past), status=400)

    # Admin
    expect("admin sees any quotation", "GET", f"/quotations/{qid}", boss.token)
    expect("unauthenticated access refused", "GET", "/quotations", None, status=401)


def journey_order():
    """Order lifecycle: create → confirm (needs address) → loading → partial → complete; cancel and delete rules; item window; who may do what."""
    boss = admin()
    owner, manager, buyer, driver, plants = team(boss)
    rival = approved_owner(boss, "Rival")
    nid = owner.nursery_id
    mango, jamun = plants
    address = {"contact_name": "Ravi", "contact_mobile": buyer.mobile, "address_line1": "12 Lake Road", "city": "Hyderabad", "state": "Telangana", "postal_code": "500032"}

    def order(**kw):
        o = {"seller_nursery_id": nid, "buyer_mobile": buyer.mobile, "buyer_name": "Ravi",
             "items": [{"plant_id": mango, "quantity": 10, "unit_price": 150, "total_price": 1500},
                       {"plant_id": jamun, "quantity": 4, "unit_price": 80, "total_price": 320}]}
        o.update(kw)
        return o

    # Validation and who may create
    expect("an order cannot start COMPLETED", "POST", "/orders", owner.token, order(order_status="COMPLETED"), status=400)
    expect("an order cannot start CONFIRMED", "POST", "/orders", owner.token, order(order_status="CONFIRMED"), status=400)
    expect("invalid buyer mobile is refused", "POST", "/orders", owner.token, order(buyer_mobile="12345"), status=400)
    expect("zero quantity is refused", "POST", "/orders", owner.token, order(items=[{"plant_id": mango, "quantity": 0, "unit_price": 1, "total_price": 0}]), status=400)
    expect("a line total that is not quantity × price is refused", "POST", "/orders", owner.token, order(items=[{"plant_id": mango, "quantity": 2, "unit_price": 10, "total_price": 5}]), status=400)
    expect("owner cannot sell from another nursery", "POST", "/orders", owner.token, order(seller_nursery_id=rival.nursery_id), status=403)
    expect("a driver cannot create orders", "POST", "/orders", driver.token, order(), status=403)
    expect("admin does not trade", "POST", "/orders", boss.token, order(), status=403)
    expect("a buyer cannot order on someone else's behalf", "POST", "/orders", buyer.token, order(buyer_mobile=None, buyer_user_id=owner.id), status=403)

    # Happy path: owner creates for the buyer
    o = expect("owner creates an order for the buyer", "POST", "/orders", owner.token, order(), status=201)["order"]
    oid = o["id"]
    check("…PENDING, ORD- code, total 1820", o["order_status"] == "PENDING" and o["order_code"].startswith("ORD-") and float(o["total_amount"]) == 1820, o)
    check("…buyer linked by mobile", o.get("buyer_user_id") == buyer.id, o.get("buyer_user_id"))
    expect("buyer sees the order", "GET", f"/orders/{oid}", buyer.token)
    out = expect("buyer's list", "GET", "/orders?buying=true", buyer.token)
    check("…includes it", oid in [x["id"] for x in out.get("orders", [])], out)
    expect("a rival owner cannot see it", "GET", f"/orders/{oid}", rival.token, status=(403, 404))
    expect("a stranger cannot see it", "GET", f"/orders/{oid}", User(first="Nosy", activity="CUSTOMER").token, status=(403, 404))
    expect("a rival owner cannot cancel it", "POST", f"/orders/{oid}/cancel", rival.token, {"reason": "x"}, status=(403, 404))
    expect("manager sees the nursery's order", "GET", f"/orders/{oid}", manager.token)
    expect("buyer cannot confirm", "POST", f"/orders/{oid}/confirm", buyer.token, {}, status=403)
    out = expect("confirm without a delivery address is refused", "POST", f"/orders/{oid}/confirm", owner.token, {}, status=(400, 409, 422))
    expect("loading cannot start from PENDING", "POST", f"/orders/{oid}/start-loading", owner.token, {}, status=409)
    expect("PENDING cannot jump to COMPLETED", "PUT", f"/orders/{oid}/status", owner.token, {"order_status": "COMPLETED"}, status=409)
    expect("status endpoint cannot cancel", "PUT", f"/orders/{oid}/status", owner.token, {"order_status": "CANCELLED"}, status=409)
    expect("unknown status is refused", "PUT", f"/orders/{oid}/status", owner.token, {"order_status": "SHIPPED"}, status=400)
    expect("owner adds the delivery address", "PUT", f"/orders/{oid}/delivery", owner.token, address)

    # Items while editable
    item = expect("owner adds an item", "POST", f"/orders/{oid}/items", owner.token, {"plant_id": jamun, "quantity": 1, "unit_price": 80, "total_price": 80}, status=(200, 201))["item"]
    expect("owner removes it again", "DELETE", f"/orders/{oid}/items/{item['id']}", owner.token, status=(200, 204))
    expect("buyer cannot edit items", "POST", f"/orders/{oid}/items", buyer.token, {"plant_id": jamun, "quantity": 1, "unit_price": 80, "total_price": 80}, status=403)

    # Manager assignment
    expect("manager cannot assign", "POST", f"/orders/{oid}/assign-manager", manager.token, {"manager_user_id": manager.id}, status=403)
    expect("assigning a non-member is refused", "POST", f"/orders/{oid}/assign-manager", owner.token, {"manager_user_id": buyer.id}, status=400)
    code = expect("owner assigns the manager", "POST", f"/orders/{oid}/assign-manager", owner.token, {"manager_user_id": manager.id})["order"]["order_code"]
    titles = []
    for _ in range(20):
        titles = [n.get("message", "") for n in call("GET", "/notifications", manager.token)[1].get("notifications", [])]
        if any(code in t for t in titles):
            break
        time.sleep(0.25)
    check("…the manager is notified", any(code in t for t in titles), titles)

    # Confirm → loading → partial → complete
    out = expect("manager confirms", "POST", f"/orders/{oid}/confirm", manager.token, {})
    check("…CONFIRMED", out["order"]["order_status"] == "CONFIRMED", out["order"]["order_status"])
    confirmed_at = out["order"].get("confirmed_at")
    check("…with the time it was accepted (order timeline)", bool(confirmed_at), out["order"].get("confirmed_at"))
    expect("confirming twice is refused", "POST", f"/orders/{oid}/confirm", manager.token, {}, status=409)
    out = expect("order after the refused second confirm", "GET", f"/orders/{oid}", owner.token)
    check("…keeps its first acceptance time", out["order"].get("confirmed_at") == confirmed_at, out["order"].get("confirmed_at"))
    expect("buyer cannot cancel a confirmed order", "POST", f"/orders/{oid}/cancel", buyer.token, {"reason": "late"}, status=403)
    expect("a confirmed order cannot be deleted", "DELETE", f"/orders/{oid}", owner.token, status=409)
    expect("loaded quantity only while loading", "PUT", f"/orders/{oid}/items/{o['items'][0]['id']}/loaded-quantity", owner.token, {"loaded_quantity": 5}, status=409)
    out = expect("start loading", "POST", f"/orders/{oid}/start-loading", owner.token, {})
    check("…LOADING", out["order"]["order_status"] == "LOADING", out["order"]["order_status"])
    items = expect("list items", "GET", f"/orders/{oid}/items", owner.token)["items"]
    first, second = items[0], items[1]
    expect("loaded more than ordered is refused", "PUT", f"/orders/{oid}/items/{first['id']}/loaded-quantity", owner.token, {"loaded_quantity": first["quantity"] + 1}, status=400)
    expect("negative loaded quantity is refused", "PUT", f"/orders/{oid}/items/{first['id']}/loaded-quantity", owner.token, {"loaded_quantity": -1}, status=400)
    expect("an item of another order is refused", "PUT", f"/orders/{oid}/items/999999999/loaded-quantity", owner.token, {"loaded_quantity": 1}, status=(403, 404))
    expect("load fewer of the first item", "PUT", f"/orders/{oid}/items/{first['id']}/loaded-quantity", owner.token, {"loaded_quantity": first["quantity"] - 2})
    expect("load all of the second item", "PUT", f"/orders/{oid}/items/{second['id']}/loaded-quantity", owner.token, {"loaded_quantity": second["quantity"]})
    expect("complete loading without the no-prohibited-items declaration is refused", "POST", f"/orders/{oid}/complete-loading", owner.token, {}, status=400)
    out = expect("complete loading", "POST", f"/orders/{oid}/complete-loading", owner.token, LOADED_OK)
    po = out["order"]
    expected_total = (first["quantity"] - 2) * first["unit_price"] + second["quantity"] * second["unit_price"]
    check("…PARTIALLY_FULFILLED and total from loaded quantities", po["order_status"] == "PARTIALLY_FULFILLED" and abs(float(po["total_amount"]) - expected_total) < 0.01,
          {"status": po["order_status"], "total": po["total_amount"], "want": expected_total})
    out = expect("buyer: loading proof", "GET", f"/orders/{oid}/loading-proof", buyer.token)["loading_proof"]
    check("…the seller's declaration, who loaded it, no truck yet", out["declared_no_prohibited_items"] and out.get("completed_by_name") and out.get("truck") is None, out)
    expect("a stranger cannot see the loading proof", "GET", f"/orders/{oid}/loading-proof", User(first="Nosy", activity="CUSTOMER").token, status=403)
    expect("items are locked after loading", "POST", f"/orders/{oid}/items", owner.token, {"plant_id": jamun, "quantity": 1, "unit_price": 80, "total_price": 80}, status=409)
    expect("a partially fulfilled order cannot be cancelled", "POST", f"/orders/{oid}/cancel", owner.token, {"reason": "x"}, status=409)
    out = expect("owner completes", "PUT", f"/orders/{oid}/status", owner.token, {"order_status": "COMPLETED"})
    check("…COMPLETED", out["order"]["order_status"] == "COMPLETED", out["order"]["order_status"])
    expect("a completed order is final", "PUT", f"/orders/{oid}/status", owner.token, {"order_status": "CONFIRMED"}, status=409)
    expect("a completed order cannot be cancelled", "POST", f"/orders/{oid}/cancel", owner.token, {"reason": "x"}, status=409)
    expect("a completed order cannot be reassigned", "POST", f"/orders/{oid}/assign-manager", owner.token, {"manager_user_id": manager.id}, status=409)

    # Full load → LOADED
    o2 = expect("second order", "POST", "/orders", owner.token, order(delivery=address), status=201)["order"]
    expect("confirm", "POST", f"/orders/{o2['id']}/confirm", owner.token, {})
    expect("start loading", "POST", f"/orders/{o2['id']}/start-loading", owner.token, {})
    out = expect("complete loading with nothing short", "POST", f"/orders/{o2['id']}/complete-loading", owner.token, LOADED_OK)
    check("…LOADED, total unchanged", out["order"]["order_status"] == "LOADED" and float(out["order"]["total_amount"]) == 1820, out["order"])
    expect("a loaded order cannot be cancelled", "POST", f"/orders/{o2['id']}/cancel", owner.token, {"reason": "x"}, status=409)

    # Cancel and delete rules
    o3 = expect("buyer places their own order", "POST", "/orders", buyer.token, order(buyer_mobile=None, delivery=address), status=201)["order"]
    check("…buyer is the actor", o3.get("buyer_user_id") == buyer.id, o3)
    expect("owner sees the buyer's order", "GET", f"/orders/{o3['id']}", owner.token)
    out = expect("buyer cancels own PENDING order", "POST", f"/orders/{o3['id']}/cancel", buyer.token, {"reason": "ordered twice"})
    check("…CANCELLED with reason", out["order"]["order_status"] == "CANCELLED" and out["order"].get("cancel_reason") == "ordered twice", out["order"])
    expect("cancelling twice is refused", "POST", f"/orders/{o3['id']}/cancel", buyer.token, {}, status=409)
    expect("a cancelled order cannot be confirmed", "POST", f"/orders/{o3['id']}/confirm", owner.token, {}, status=409)
    o4 = expect("order to cancel while loading", "POST", "/orders", owner.token, order(delivery=address), status=201)["order"]
    expect("confirm", "POST", f"/orders/{o4['id']}/confirm", owner.token, {})
    expect("start loading", "POST", f"/orders/{o4['id']}/start-loading", owner.token, {})
    expect("manager cancels during loading", "POST", f"/orders/{o4['id']}/cancel", manager.token, {"reason": "truck broke"})
    o5 = expect("order to delete", "POST", "/orders", owner.token, order(), status=201)["order"]
    expect("buyer cannot delete", "DELETE", f"/orders/{o5['id']}", buyer.token, status=403)
    expect("owner deletes a PENDING order", "DELETE", f"/orders/{o5['id']}", owner.token, status=(200, 204))
    expect("a deleted order is gone", "GET", f"/orders/{o5['id']}", owner.token, status=404)

    # A manager who leaves loses the nursery's orders, even as the assignee
    o6 = expect("order assigned to the manager", "POST", "/orders", owner.token, order(delivery=address), status=201)["order"]
    expect("assign", "POST", f"/orders/{o6['id']}/assign-manager", owner.token, {"manager_user_id": manager.id})
    expect("manager leaves", "DELETE", "/nurseries/me/leave", manager.token, status=(200, 204))
    expect("former manager cannot view the order", "GET", f"/orders/{o6['id']}", manager.token, status=(401, 403, 404))
    expect("former manager cannot confirm it", "POST", f"/orders/{o6['id']}/confirm", manager.token, {}, status=(401, 403, 404))

    expect("admin sees any order", "GET", f"/orders/{oid}", boss.token)


def journey_dispatch():
    """Dispatch + tracking: create only for loaded orders; driver QR accept; one active trip per driver; who may move the trip; GPS access; delivery completes the order."""
    boss = admin()
    owner, manager, buyer, driver, plants = team(boss)
    rival = approved_owner(boss, "Rival")
    second_driver = approved_driver(boss, owner)
    nid = owner.nursery_id
    address = {"contact_name": "Ravi", "contact_mobile": buyer.mobile, "address_line1": "12 Lake Road", "city": "Hyderabad", "latitude": 17.44, "longitude": 78.38}

    def loaded_order():
        o = expect("order", "POST", "/orders", owner.token, {"seller_nursery_id": nid, "buyer_mobile": buyer.mobile, "delivery": address,
                    "items": [{"plant_id": plants[0], "quantity": 5, "unit_price": 100, "total_price": 500}]}, status=201)["order"]
        expect("confirm", "POST", f"/orders/{o['id']}/confirm", owner.token, {})
        return o

    o = loaded_order()
    expect("no dispatch before loading is done", "POST", "/dispatches", owner.token, {"order_id": o["id"]}, status=422)
    expect("start loading", "POST", f"/orders/{o['id']}/start-loading", owner.token, {})
    expect("complete loading", "POST", f"/orders/{o['id']}/complete-loading", owner.token, LOADED_OK)
    expect("buyer cannot create a dispatch", "POST", "/dispatches", buyer.token, {"order_id": o["id"]}, status=403)
    expect("driver cannot create a dispatch", "POST", "/dispatches", driver.token, {"order_id": o["id"]}, status=403)
    expect("a rival owner cannot dispatch this order", "POST", "/dispatches", rival.token, {"order_id": o["id"]}, status=403)
    expect("unknown order", "POST", "/dispatches", owner.token, {"order_id": 999999999}, status=(400, 404))
    d = expect("manager creates the dispatch", "POST", "/dispatches", manager.token, {"order_id": o["id"]}, status=201)["dispatch"]
    did, code = d["id"], d["dispatch_code"]
    trip = "TRIP-" + str(d.get("trip_uuid"))  # what the nursery's QR carries
    check("…PENDING with a code and tracking link", d["dispatch_status"] == "PENDING" and code and d.get("trip_uuid"), d)
    expect("only one active dispatch per order", "POST", "/dispatches", owner.token, {"order_id": o["id"]}, status=409)

    # A retry after a lost response (weak network) must not create a second order
    body = {"seller_nursery_id": nid, "buyer_mobile": buyer.mobile, "delivery": address,
            "items": [{"plant_id": plants[0], "quantity": 2, "unit_price": 100, "total_price": 200}]}
    key = {"Idempotency-Key": "journey-" + str(random.randint(10**8, 10**9))}
    a = expect("create an order with an Idempotency-Key", "POST", "/orders", owner.token, body, status=201, headers=key)["order"]
    b = expect("…the same request again (retry)", "POST", "/orders", owner.token, body, status=201, headers=key)["order"]
    check("…replays the first order instead of creating a second", a["id"] == b["id"], (a["id"], b["id"]))
    c = expect("…a new key is a new order", "POST", "/orders", owner.token, body, status=201,
               headers={"Idempotency-Key": key["Idempotency-Key"] + "x"})["order"]
    check("…different id", c["id"] != a["id"], c["id"])
    expect("…the same key from another user is theirs alone", "POST", "/orders", rival.token, dict(body, seller_nursery_id=rival.nursery_id), status=(201, 400, 403), headers=key)

    # QR lookup and accept
    expect("the order's buyer can look up the trip", "GET", f"/dispatches/code/{trip}", buyer.token)
    expect("an unrelated buyer cannot look up a trip by code", "GET", f"/dispatches/code/{trip}", User(first="Nosy", activity="CUSTOMER").token, status=403)
    expect("a rival owner cannot look up a trip by code", "GET", f"/dispatches/code/{trip}", rival.token, status=403)
    expect("a driver previews the unclaimed trip", "GET", f"/dispatches/code/{trip}", driver.token)
    expect("…but a guessed dispatch code finds nothing", "GET", f"/dispatches/code/{code}", driver.token, status=404)
    expect("…nor a bare id", "GET", f"/dispatches/code/{did}", driver.token, status=404)
    expect("a driver cannot accept by id alone", "POST", f"/dispatches/{did}/accept", driver.token, {}, status=404)
    expect("…nor with the dispatch code", "POST", f"/dispatches/{did}/accept", driver.token, {"trip_code": code}, status=404)
    expect("a buyer cannot accept a trip", "POST", f"/dispatches/{did}/accept", buyer.token, {"trip_code": trip}, status=403)
    expect("an owner cannot accept a trip", "POST", f"/dispatches/{did}/accept", owner.token, {"trip_code": trip}, status=403)
    out = expect("driver accepts with the QR's trip code", "POST", f"/dispatches/{did}/accept", driver.token, {"trip_code": trip})
    check("…ACCEPTED by this driver", out["dispatch"]["dispatch_status"] == "ACCEPTED" and out["dispatch"].get("driver_user_id") == driver.id, out["dispatch"])
    check("…the driver sees which nursery to pick up from", bool(out["dispatch"].get("seller_nursery_name")), out["dispatch"])
    expect("accepting again is idempotent", "POST", f"/dispatches/{did}/accept", driver.token, {"trip_code": trip})
    expect("another driver cannot take an accepted trip", "POST", f"/dispatches/{did}/accept", second_driver.token, {"trip_code": trip}, status=(403, 409))
    expect("another driver cannot look it up any more", "GET", f"/dispatches/code/{trip}", second_driver.token, status=403)

    # One active trip per driver
    o2 = loaded_order()
    expect("start loading", "POST", f"/orders/{o2['id']}/start-loading", owner.token, {})
    expect("complete loading", "POST", f"/orders/{o2['id']}/complete-loading", owner.token, LOADED_OK)
    d2 = expect("second dispatch", "POST", "/dispatches", owner.token, {"order_id": o2["id"]}, status=201)["dispatch"]
    expect("a driver with an active trip cannot accept another", "POST", f"/dispatches/{d2['id']}/accept", driver.token, {"trip_code": d2["trip_uuid"]}, status=409)

    # Moving the trip
    st = lambda who, status, code=200: expect(f"{who[0]} sets {status}", "PUT", f"/dispatches/{did}/status", who[1].token, {"dispatch_status": status}, status=code)
    st(("buyer", buyer), "DELIVERED", 403)
    st(("buyer", buyer), "CANCELLED", 403)
    st(("driver", driver), "CANCELLED", 403)
    st(("rival", rival), "DISPATCHED", 403)
    st(("driver", driver), "DELIVERED", 422)
    expect("no GPS points before the trip is in transit", "POST", "/tracking", driver.token, {"dispatch_id": did, "latitude": 17.41, "longitude": 78.39}, status=409)
    expect("unknown status", "PUT", f"/dispatches/{did}/status", owner.token, {"dispatch_status": "FLYING"}, status=400)
    expect("no trip events before the trip starts", "POST", f"/dispatches/{d2['id']}/trip-events", owner.token, {"event_type": "DELAY"}, status=403)
    st(("driver", driver), "DISPATCHED")
    expect("trip event needs a type", "POST", f"/dispatches/{did}/trip-events", driver.token, {"event_type": " "}, status=400)
    expect("trip event with impossible coordinates", "POST", f"/dispatches/{did}/trip-events", driver.token, {"event_type": "DELAY", "latitude": 123, "longitude": 78}, status=400)
    expect("driver logs a trip event", "POST", f"/dispatches/{did}/trip-events", driver.token, {"event_type": "fuel stop", "latitude": 17.40, "longitude": 78.40}, status=(200, 201))
    expect("buyer cannot log trip events", "POST", f"/dispatches/{did}/trip-events", buyer.token, {"event_type": "DELAY"}, status=403)
    expect("another driver cannot log trip events", "POST", f"/dispatches/{did}/trip-events", second_driver.token, {"event_type": "DELAY"}, status=403)

    st(("driver", driver), "IN_TRANSIT")

    # GPS tracking
    expect("driver sends a GPS point", "POST", "/tracking", driver.token, {"dispatch_id": did, "latitude": 17.41, "longitude": 78.39}, status=(200, 201))
    expect("GPS point with impossible latitude", "POST", "/tracking", driver.token, {"dispatch_id": did, "latitude": 91, "longitude": 78}, status=400)
    expect("buyer cannot send GPS points", "POST", "/tracking", buyer.token, {"dispatch_id": did, "latitude": 17.4, "longitude": 78.4}, status=403)
    expect("another driver cannot track this trip", "POST", "/tracking", second_driver.token, {"dispatch_id": did, "latitude": 17.4, "longitude": 78.4}, status=403)
    expect("driver shares live location", "POST", "/tracking/live", driver.token, {"dispatch_id": did, "latitude": 17.42, "longitude": 78.38}, status=(200, 201))
    for who in [("owner", owner), ("buyer", buyer), ("driver", driver)]:
        expect(f"{who[0]} reads the trip's latest position", "GET", f"/dispatches/{did}/tracking/latest", who[1].token)
    expect("a rival owner cannot read the trip's GPS", "GET", f"/dispatches/{did}/tracking", rival.token, status=403)
    expect("a stranger cannot read the trip's GPS", "GET", f"/dispatches/{did}/tracking", User(first="Nosy", activity="CUSTOMER").token, status=403)
    code_pub, pub = call("GET", f"/track/{d['trip_uuid']}")
    check("public tracking link works without login", code_pub == 200, code_pub)
    check("…and does not expose the buyer's mobile", buyer.mobile not in json.dumps(pub), pub)
    pub_trip = (pub or {}).get("dispatch", {})
    leaked = sorted(set(pub_trip) & {"destination_address", "delivery_latitude", "delivery_longitude", "driver_mobile",
                                     "driver_name", "customer_user_id", "driver_user_id", "notes", "order_id", "id"})
    check("…nor the delivery address, coordinates, driver's phone or internal ids", not leaked, leaked)
    expect("an unknown tracking link is not found", "GET", "/track/00000000-0000-0000-0000-000000000000", None, status=(400, 404))

    expect("order cannot complete while the trip is open", "PUT", f"/orders/{o['id']}/status", owner.token, {"order_status": "COMPLETED"}, status=409)
    # Delivery needs the customer: their code, or their "received" tap
    st(("driver", driver), "DELIVERED", 422)
    bd = expect("buyer opens the trip on its way", "GET", f"/dispatches/{did}", buyer.token)["dispatch"]
    code4 = bd.get("delivery_code") or ""
    check("…the customer sees a 4-digit delivery code", len(code4) == 4 and code4.isdigit(), bd)
    for who in [("owner", owner), ("manager", manager), ("driver", driver)]:
        seen = expect(f"{who[0]} opens the trip", "GET", f"/dispatches/{did}", who[1].token)["dispatch"]
        check(f"…the {who[0]} never sees the code", "delivery_code" not in seen, seen)
    _, pub = call("GET", f"/track/{d['trip_uuid']}")
    check("…nor does the public tracking link", "delivery_code" not in json.dumps(pub), pub)
    wrong = "0000" if code4 != "0000" else "1111"
    deliver = lambda what, who, body, status=200: expect(what, "POST", f"/dispatches/{did}/deliver", who.token, body, status=status)
    deliver("a wrong code is refused", driver, {"code": wrong}, 422)
    deliver("a driver cannot deliver without the code", driver, {"override_reason": "Customer did not pick up the phone"}, 422)
    deliver("the customer cannot use deliver", buyer, {"code": code4}, 403)
    deliver("a rival owner cannot deliver", rival, {"code": code4}, 403)
    out = deliver("driver delivers with the customer's code", driver, {"code": code4})["dispatch"]
    check("…DELIVERED, confirmed by code", out["dispatch_status"] == "DELIVERED" and out.get("delivery_confirmation") == "CUSTOMER_CODE", out)
    titles = []
    for _ in range(20):
        titles = [n.get("message", "") for n in call("GET", "/notifications", owner.token)[1].get("notifications", [])]
        if any(out["dispatch_code"] in t and "delivered" in t for t in titles):
            break
        time.sleep(0.25)
    check("…the owner is told it was delivered", any(out["dispatch_code"] in t and "delivered" in t for t in titles), titles)
    deliver("delivering twice is refused", owner, {"code": code4}, 422)
    bd = expect("buyer opens the delivered trip", "GET", f"/dispatches/{did}", buyer.token)["dispatch"]
    check("…the code is gone; the customer can report a problem", "delivery_code" not in bd and bd["capabilities"]["can_report_issue"], bd)
    receipt = lambda what, who, body, status=200: expect(what, "POST", f"/dispatches/{did}/receipt", who.token, body, status=status)
    receipt("the seller cannot report for the customer", owner, {"issue": "x"}, 403)
    out = receipt("buyer reports a problem", buyer, {"issue": "3 plants broken"})["dispatch"]
    check("…recorded", out.get("delivery_issue") == "3 plants broken", out)
    receipt("only one problem report", buyer, {"issue": "again"}, 409)
    out = expect("buyer sees the order", "GET", f"/orders/{o['id']}", buyer.token)
    check("…delivery completed the order", out["order"]["order_status"] == "COMPLETED", out["order"]["order_status"])
    st(("owner", owner), "CANCELLED", 422)
    expect("no trip events after delivery", "POST", f"/dispatches/{did}/trip-events", driver.token, {"event_type": "LATE"}, status=422)
    expect("second driver is free to accept the other trip", "POST", f"/dispatches/{d2['id']}/accept", second_driver.token, {"trip_code": d2["trip_uuid"]})
    expect("owner cancels the other trip", "PUT", f"/dispatches/{d2['id']}/status", owner.token, {"dispatch_status": "CANCELLED"})

    # Outside lorry (driver without the app): the nursery confirms with the customer's code
    o3 = loaded_order()
    expect("start loading", "POST", f"/orders/{o3['id']}/start-loading", owner.token, {})
    expect("complete loading", "POST", f"/orders/{o3['id']}/complete-loading", owner.token, LOADED_OK)
    expect("an outside lorry needs its vehicle number", "POST", "/dispatches", owner.token, {"order_id": o3["id"], "is_outside_transport": True}, status=400)
    d3 = expect("owner sends it on an outside lorry", "POST", "/dispatches", owner.token, {"order_id": o3["id"], "is_outside_transport": True,
                "outside_vehicle_number": "ap05 tb 1234", "outside_driver_name": "Ramesh", "outside_driver_mobile": "+91 98765 01234"}, status=201)["dispatch"]
    check("…no QR to share, vehicle and driver recorded", d3["is_outside_transport"] and not d3["capabilities"]["can_share_code"]
          and d3.get("outside_vehicle_number") == "AP05 TB 1234" and d3.get("outside_driver_mobile") == "9876501234", d3)
    expect("no app driver can join an outside lorry", "POST", f"/dispatches/{d3['id']}/accept", second_driver.token, {"trip_code": d3["trip_uuid"]}, status=409)
    expect("owner marks it dispatched", "PUT", f"/dispatches/{d3['id']}/status", owner.token, {"dispatch_status": "DISPATCHED"})
    code3 = expect("buyer opens it", "GET", f"/dispatches/{d3['id']}", buyer.token)["dispatch"].get("delivery_code") or ""
    wrong3 = "0000" if code3 != "0000" else "1111"
    for i in range(4):
        expect(f"wrong code {i + 1}", "POST", f"/dispatches/{d3['id']}/deliver", owner.token, {"code": wrong3}, status=422)
    expect("the 5th wrong code locks it", "POST", f"/dispatches/{d3['id']}/deliver", owner.token, {"code": wrong3}, status=409)
    expect("…even the right code no longer works", "POST", f"/dispatches/{d3['id']}/deliver", owner.token, {"code": code3}, status=409)
    bd = expect("buyer opens it", "GET", f"/dispatches/{d3['id']}", buyer.token)["dispatch"]
    check("…a locked code is not shown", "delivery_code" not in bd, bd)
    expect("an override needs a real reason", "POST", f"/dispatches/{d3['id']}/deliver", owner.token, {"override_reason": "ok"}, status=400)
    out = expect("owner marks it delivered without the code, with a reason", "POST", f"/dispatches/{d3['id']}/deliver", owner.token,
                 {"override_reason": "Customer's phone was switched off; plants handed to his son"})["dispatch"]
    check("…UNCONFIRMED, reason kept", out.get("delivery_confirmation") == "UNCONFIRMED" and "son" in (out.get("delivery_override_reason") or ""), out)
    bd = expect("buyer opens it", "GET", f"/dispatches/{d3['id']}", buyer.token)["dispatch"]
    check("…the customer can confirm or dispute", bd["capabilities"]["can_confirm_receipt"] and bd["capabilities"]["can_report_issue"], bd["capabilities"])
    out = expect("buyer confirms they received it", "POST", f"/dispatches/{d3['id']}/receipt", buyer.token, {})["dispatch"]
    check("…now confirmed in the app", out.get("delivery_confirmation") == "CUSTOMER_APP", out)

    # Ratings: only the parties, only once it is done
    stranger = User(first="Nosy", activity="CUSTOMER")
    good = {"plant_quality_rating": 5, "communication_rating": 4, "overall_experience_rating": 5, "would_buy_again": True}
    expect("a stranger cannot rate someone else's order", "POST", f"/ratings/order/{o['id']}", stranger.token, good, status=403)
    expect("an order not yet completed cannot be rated", "POST", f"/ratings/order/{o2['id']}", buyer.token, good, status=409)
    expect("scores are 1 to 5", "POST", f"/ratings/order/{o['id']}", buyer.token, dict(good, plant_quality_rating=9), status=400)
    expect("an unknown order cannot be rated", "POST", "/ratings/order/999999999", buyer.token, good, status=404)
    expect("the buyer rates their completed order", "POST", f"/ratings/order/{o['id']}", buyer.token, good)
    trip = {"driver_behaviour_rating": 5, "on_time_delivery_rating": 4, "plant_condition_rating": 5}
    expect("a rival owner cannot rate this trip", "POST", f"/ratings/trip/{did}", rival.token, trip, status=403)
    expect("a stranger cannot rate this trip", "POST", f"/ratings/trip/{did}", stranger.token, trip, status=403)
    expect("a cancelled trip cannot be rated", "POST", f"/ratings/trip/{d2['id']}", owner.token, trip, status=409)
    expect("the buyer rates the delivered trip", "POST", f"/ratings/trip/{did}", buyer.token, trip)
    expect("the seller rates the delivered trip", "POST", f"/ratings/trip/{did}", owner.token, trip)

    # Lists are scoped
    out = expect("driver's list", "GET", "/dispatches", driver.token)
    ids = [x["id"] for x in out.get("dispatches", [])]
    check("…only their trips", did in ids and d2["id"] not in ids, ids)
    out = expect("buyer's list", "GET", "/dispatches", buyer.token)
    check("…dispatches of their orders", did in [x["id"] for x in out.get("dispatches", [])], out)
    expect("rival's dispatches for this order", "GET", f"/orders/{o['id']}/dispatches", rival.token, status=(403, 404))


def journey_payments():
    """Payments: the seller (or admin) records money, never the payer; status machine; cancelled orders take no payments; who can see what."""
    boss = admin()
    owner, manager, buyer, driver, plants = team(boss)
    rival = approved_owner(boss, "Rival")
    o = expect("order", "POST", "/orders", owner.token, {"seller_nursery_id": owner.nursery_id, "buyer_mobile": buyer.mobile,
               "items": [{"plant_id": plants[0], "quantity": 4, "unit_price": 250, "total_price": 1000}]}, status=201)["order"]
    pay = lambda **kw: dict({"payment_for": "ORDER", "order_id": o["id"], "amount": 1000, "payment_method": "UPI"}, **kw)

    expect("buyer cannot record their own payment", "POST", "/payments/manual", buyer.token, pay(), status=403)
    expect("a rival owner cannot record a payment", "POST", "/payments/manual", rival.token, pay(), status=403)
    expect("a driver cannot record a payment", "POST", "/payments/manual", driver.token, pay(), status=403)
    expect("zero amount is refused", "POST", "/payments/manual", owner.token, pay(amount=0), status=400)
    expect("unknown method is refused", "POST", "/payments/manual", owner.token, pay(payment_method="BITCOIN"), status=400)
    expect("order payment needs an order", "POST", "/payments/manual", owner.token, pay(order_id=None), status=400)
    p = expect("owner records a pending cash payment", "POST", "/payments/manual", owner.token, pay(payment_method="CASH", payment_status="PENDING"), status=201)["payment"]
    check("…PENDING, PAY- code, payer is the buyer", p["payment_status"] == "PENDING" and p["payment_code"].startswith("PAY-") and p.get("payer_user_id") == buyer.id, p)
    expect("buyer sees the payment", "GET", f"/payments/{p['id']}", buyer.token)
    expect("buyer cannot mark it paid", "PUT", f"/payments/{p['id']}/status", buyer.token, {"payment_status": "SUCCESS"}, status=403)
    expect("a rival owner cannot see it", "GET", f"/payments/{p['id']}", rival.token, status=403)
    expect("owner marks it paid", "PUT", f"/payments/{p['id']}/status", owner.token, {"payment_status": "SUCCESS", "transaction_reference": "CASH-001"})
    expect("re-saving SUCCESS with a note is fine", "PUT", f"/payments/{p['id']}/status", owner.token, {"payment_status": "SUCCESS", "notes": "receipt 12"})
    expect("a successful payment cannot go back to PENDING", "PUT", f"/payments/{p['id']}/status", owner.token, {"payment_status": "PENDING"}, status=409)
    expect("a successful payment cannot be marked FAILED", "PUT", f"/payments/{p['id']}/status", owner.token, {"payment_status": "FAILED"}, status=409)
    expect("owner refunds", "PUT", f"/payments/{p['id']}/status", owner.token, {"payment_status": "REFUNDED"})
    expect("a refund is final", "PUT", f"/payments/{p['id']}/status", owner.token, {"payment_status": "SUCCESS"}, status=409)
    out = expect("payments of the order", "GET", f"/orders/{o['id']}/payments", buyer.token)
    check("…listed for the buyer", p["id"] in [x["id"] for x in out.get("payments", [])], out)
    q = expect("a payment with a gateway payload", "POST", "/payments/manual", owner.token,
               pay(payment_method="UPI", provider_signature="sig-abc", raw_response={"email": "payer@example.com", "vpa": "payer@upi"}), status=201)["payment"]
    shown = json.dumps([q, expect("…read back", "GET", f"/payments/{q['id']}", buyer.token),
                        expect("…in the list", "GET", f"/orders/{o['id']}/payments", buyer.token)])
    check("…never returns the signature or the raw payload (payer email, UPI id)",
          "sig-abc" not in shown and "payer@" not in shown and "raw_response" not in shown and "provider_signature" not in shown, shown[:300])
    expect("cancel the order", "POST", f"/orders/{o['id']}/cancel", owner.token, {"reason": "journey"})
    expect("a cancelled order takes no payments", "POST", "/payments/manual", owner.token, pay(), status=409)
    sub = expect("owner's subscription", "GET", "/subscriptions/me", owner.token)["subscriptions"][0]
    expect("an owner cannot record a payment for their own subscription", "POST", "/payments/manual", owner.token,
           {"payment_for": "SUBSCRIPTION", "user_subscription_id": sub["id"], "amount": 999, "payment_method": "UPI"}, status=403)


def journey_market():
    """Local Market access: only nursery staff; only the posting nursery edits or archives; no self-enquiry; reports go to admin."""
    boss = admin()
    owner, manager, buyer, driver, _ = team(boss)
    rival = approved_owner(boss, "Rival")

    # The profile photo is shown on ads, not required: posting works without one.
    shy = approved_owner(boss, "No Selfie", selfie=False)
    expect("posting without a profile photo works", "POST", "/market/ads", shy.token, {"plant_name": "Rose", "title": "No face"}, status=201)
    photo = upload_selfie("the owner takes a selfie", shy)
    expect("a profile save keeps the selfie", "PUT", "/users/me", shy.token, {"first_name": "Shy", "last_name": "Owner"})
    me = expect("…check", "GET", "/users/me", shy.token)
    check("…photo still there", me.get("user", me).get("profile_image_url") == photo, me)
    expect("a profile save cannot point the photo elsewhere", "PUT", "/users/me", shy.token, {"first_name": "Shy", "profile_image_url": "https://evil.example/x.jpg"})
    me = expect("…check", "GET", "/users/me", shy.token)
    check("…photo unchanged", me.get("user", me).get("profile_image_url") == photo, me)
    me = expect("the owner deletes the selfie", "DELETE", "/users/me/avatar", shy.token)
    check("…photo gone", not me.get("user", me).get("profile_image_url"), me)

    # Guava is in seed-master, so this holds on a freshly reset DB.
    guava = expect("look up a catalog plant", "GET", "/plants/lookup?q=Psidium%20guajava", owner.token)["results"][0]["plant"]
    ad = {"plant_id": guava["id"], "plant_name": "Peru", "title": "Sweet grafts", "description": "2 ft grafts", "quantity": 200, "price_per_unit": 120, "price_unit": "PER_PLANT", "photos": []}
    expect("a buyer cannot browse the market", "GET", "/market/ads", buyer.token, status=403)
    expect("a driver cannot browse the market", "GET", "/market/ads", driver.token, status=403)
    expect("a buyer cannot post an ad", "POST", "/market/ads", buyer.token, ad, status=403)
    a = expect("manager posts an ad", "POST", "/market/ads", manager.token, ad, status=201)["ad"]
    check("…DRAFT", a["status"] == "DRAFT", a["status"])
    check("…shows who posted it, with their selfie", a.get("posted_by_name") and a.get("posted_by_photo"), a)
    check("…pickup defaults to the nursery's address", "Plot 1" in (a.get("pickup_address") or ""), a.get("pickup_address"))
    expect("a rival cannot publish it", "POST", f"/market/ads/{a['id']}/publish", rival.token, {}, status=(403, 404))
    expect("owner publishes", "POST", f"/market/ads/{a['id']}/publish", owner.token, {})
    out = expect("a rival browses the market", "GET", "/market/ads", rival.token)
    check("…and sees the ad", a["id"] in [x["id"] for x in out.get("ads", [])], out)
    out = expect("search by the catalog's botanical name", "GET", "/market/ads?q=psidium", rival.token)
    check("…finds the ad linked to that plant", a["id"] in [x["id"] for x in out.get("ads", [])], out)
    expect("a rival cannot archive it", "POST", f"/market/ads/{a['id']}/archive", rival.token, {}, status=(403, 404))
    expect("the posting nursery cannot enquire on its own ad", "POST", f"/market/ads/{a['id']}/enquiries", owner.token, {"message": "self"}, status=400)
    e = expect("a rival enquires", "POST", f"/market/ads/{a['id']}/enquiries", rival.token, {"message": "Need 50, can you deliver?"}, status=(200, 201))
    expect("a second enquiry from the same nursery is refused", "POST", f"/market/ads/{a['id']}/enquiries", rival.token, {"message": "again"}, status=400)
    got = expect("…the owner's view of the ad", "GET", f"/market/ads/{a['id']}", owner.token)["ad"]
    check("…can no longer be deleted (it has a chat)", got["capabilities"]["can_delete"] is False, got["capabilities"])
    expect("…delete is refused: archive it instead", "DELETE", f"/market/ads/{a['id']}", owner.token, status=409)
    eid = e.get("enquiry", e).get("id")
    expect("manager replies", "POST", f"/market/enquiries/{eid}/reply", manager.token, {"body": "Yes, Rs 120 each"}, status=(200, 201))
    inbox = expect("the rival's chat list", "GET", "/market/enquiries?direction=sent", rival.token)
    row = next(x for x in inbox["enquiries"] if x["id"] == eid)
    check("…shows the reply as unread, with a preview", row["unread_count"] == 1 and row.get("last_message") == "Yes, Rs 120 each" and inbox["unread_total"] >= 1, row)
    expect("the rival opens the chat", "GET", f"/market/enquiries/{eid}", rival.token)
    inbox = expect("…and the list again", "GET", "/market/enquiries?direction=sent", rival.token)
    check("…now read", next(x for x in inbox["enquiries"] if x["id"] == eid)["unread_count"] == 0, inbox["unread_total"])
    seller = expect("the rival opens the seller's profile", "GET", f"/market/nurseries/{owner.nursery_id}", rival.token)["seller"]
    check("…name, owner selfie, activity and the live ad", seller["nursery_name"] and seller["owner_photo"] and seller["live_ads"] >= 1
          and a["id"] in [x["id"] for x in seller["ads"]], seller)
    check("…and its top items (team() lists mango and neem)", len(seller["top_items"]) >= 2
          and all(t["plant_name"] and t["size_name"] for t in seller["top_items"]), seller.get("top_items"))
    expect("a buyer cannot open seller profiles", "GET", f"/market/nurseries/{owner.nursery_id}", buyer.token, status=403)
    out = expect("the seller lists this ad's chats", "GET", f"/market/enquiries?direction=received&ad_id={a['id']}", owner.token)
    check("…exactly the rival's chat", [x["id"] for x in out["enquiries"]] == [eid] and out["total"] == 1, out)
    out = expect("…filtered by status too", "GET", f"/market/enquiries?direction=received&ad_id={a['id']}&status=IN_PROGRESS&per_page=5", owner.token)
    check("…count and page agree", out["total"] == len(out["enquiries"]) == 1, out)
    out = expect("…another ad has none", "GET", f"/market/enquiries?direction=received&ad_id={a['id'] + 999999}", owner.token)
    check("…empty", out["total"] == 0 and out["enquiries"] == [], out)
    seen = expect("the rival reopens the ad", "GET", f"/market/ads/{a['id']}", rival.token)["ad"]
    check("…and is pointed at its existing chat", seen.get("my_enquiry_id") == eid and not seen["capabilities"]["can_enquire"], seen)

    # Bargaining: offer, counter, accept. Offers in a chat replace each other.
    chat = f"/market/enquiries/{eid}"
    offer = expect("the rival offers Rs 100 for 50", "POST", f"{chat}/offers", rival.token, {"price_per_unit": 100, "quantity": 50}, status=201)["message"]
    check("…a pending offer", offer["type"] == "OFFER" and offer["offer_status"] == "PENDING", offer)
    expect("an offer above the ad's quantity is refused", "POST", f"{chat}/offers", rival.token, {"price_per_unit": 100, "quantity": 201}, status=400)
    expect("a zero offer is refused", "POST", f"{chat}/offers", rival.token, {"price_per_unit": 0}, status=400)
    expect("an offer under 70% of the asking price is refused", "POST", f"{chat}/offers", rival.token, {"price_per_unit": 83.99}, status=400)
    expect("the rival cannot accept its own offer", "POST", f"{chat}/offers/{offer['id']}/accept", rival.token, {}, status=409)
    counter = expect("the manager counters at Rs 110", "POST", f"{chat}/offers", manager.token, {"price_per_unit": 110, "quantity": 50}, status=201)["message"]
    view = expect("the rival opens the chat", "GET", chat, rival.token)["enquiry"]
    by_id = {m["id"]: m for m in view["messages"]}
    check("…its own offer was withdrawn by the counter", by_id[offer["id"]]["offer_status"] == "WITHDRAWN", by_id[offer["id"]])
    check("…and it can answer the counter", by_id[counter["id"]]["can_respond"] and not by_id[counter["id"]]["is_mine"], by_id[counter["id"]])
    expect("the withdrawn offer can no longer be accepted", "POST", f"{chat}/offers/{offer['id']}/accept", owner.token, {}, status=409)
    deal = expect("the rival accepts the counter", "POST", f"{chat}/offers/{counter['id']}/accept", rival.token, {})["enquiry"]
    check("…deal agreed at Rs 110 × 50", deal["status"] == "DEAL_AGREED" and float(deal["agreed_price_per_unit"]) == 110 and deal["agreed_quantity"] == 50, deal)
    check("…with a deal note in the chat", deal["messages"][-1]["type"] == "SYSTEM" and "Deal agreed" in deal["messages"][-1]["body"], deal["messages"][-1])
    expect("accepting twice is refused", "POST", f"{chat}/offers/{counter['id']}/accept", rival.token, {}, status=409)
    expect("no new offers after a deal", "POST", f"{chat}/offers", rival.token, {"price_per_unit": 90}, status=409)
    expect("the chat stays open to arrange pickup", "POST", f"{chat}/reply", rival.token, {"body": "Coming Monday"}, status=201)

    # Read-only while the ad is not live.
    expect("owner pauses the ad", "POST", f"/market/ads/{a['id']}/pause", owner.token, {})
    expect("nobody can write in a paused ad's chat", "POST", f"{chat}/reply", rival.token, {"body": "hello?"}, status=409)
    view = expect("the rival opens the paused chat", "GET", chat, rival.token)["enquiry"]
    check("…read-only with a reason", not view["capabilities"]["can_reply"] and view["capabilities"].get("read_only_reason"), view["capabilities"])
    expect("owner resumes the ad", "POST", f"/market/ads/{a['id']}/resume", owner.token, {})
    expect("the chat is writable again", "POST", f"{chat}/reply", manager.token, {"body": "Back on"}, status=201)
    expect("a buyer cannot read the enquiry", "GET", f"/market/enquiries/{eid}", buyer.token, status=403)
    expect("a rival reports the ad", "POST", f"/market/ads/{a['id']}/report", rival.token, {"reason": "SPAM"})
    expect("reporting twice is harmless (one report per nursery user)", "POST", f"/market/ads/{a['id']}/report", rival.token, {"reason": "SPAM"})
    expect("owners cannot see the moderation queue", "GET", "/market/reports", owner.token, status=403)
    expect("admin sees the moderation queue", "GET", "/market/reports?status=PENDING", boss.token)
    expect("owner archives the ad", "POST", f"/market/ads/{a['id']}/archive", owner.token, {})
    expect("an archived ad's chat is read-only", "POST", f"{chat}/reply", rival.token, {"body": "still there?"}, status=409)
    expect("the buyer cannot close the chat", "POST", f"{chat}/close", rival.token, {}, status=403)
    expect("the seller closes the chat", "POST", f"{chat}/close", owner.token, {})
    expect("a closed chat cannot be cancelled", "POST", f"{chat}/cancel", rival.token, {}, status=409)
    expect("an archived ad cannot be deleted", "DELETE", f"/market/ads/{a['id']}", owner.token, status=409)

    # Soft delete: drafts and live ads nobody has chatted on.
    d = expect("owner saves a draft", "POST", "/market/ads", owner.token, dict(ad, title="Draft to bin"), status=201)["ad"]
    check("…can be deleted", d["capabilities"]["can_delete"] is True, d["capabilities"])
    expect("a rival cannot delete it", "DELETE", f"/market/ads/{d['id']}", rival.token, status=(403, 404))
    expect("owner deletes the draft", "DELETE", f"/market/ads/{d['id']}", owner.token, status=204)
    expect("…it is gone for the owner", "GET", f"/market/ads/{d['id']}", owner.token, status=404)
    mine = expect("…and not in My Ads", "GET", "/market/ads/mine?per_page=50", owner.token)
    check("…not listed", d["id"] not in [x["id"] for x in mine["ads"]], mine["total"])
    expect("deleting twice is a 404", "DELETE", f"/market/ads/{d['id']}", owner.token, status=404)
    kept = expect("admin still has it on record", "GET", "/market/admin/ads?status=DELETED&per_page=50", boss.token)
    check("…as DELETED", d["id"] in [x["id"] for x in kept["ads"]], kept)
    gone = next(x for x in kept["ads"] if x["id"] == d["id"])
    check("…with who deleted it and when", gone.get("deleted_at") and gone.get("deleted_by_name"), gone)
    live = expect("owner posts another ad", "POST", "/market/ads", owner.token, dict(ad, title="Live, no chats"), status=201)["ad"]
    expect("…publishes it", "POST", f"/market/ads/{live['id']}/publish", owner.token, {})
    expect("…and deletes it before anyone chats", "DELETE", f"/market/ads/{live['id']}", owner.token, status=204)
    out = expect("a rival browses again", "GET", "/market/ads?per_page=50", rival.token)
    check("…the deleted ad is not listed", live["id"] not in [x["id"] for x in out["ads"]], out["total"])
    expect("…cannot open it", "GET", f"/market/ads/{live['id']}", rival.token, status=404)
    expect("…cannot chat on it", "POST", f"/market/ads/{live['id']}/enquiries", rival.token, {"message": "hi"}, status=404)

    # Photos (max 3) and pickup access, for plants you have only.
    expect("a bare-number size (no unit) is refused", "POST", "/market/ads", owner.token, dict(ad, size_description="44"), status=400)
    sized = expect("a catalog size is accepted", "POST", "/market/ads", owner.token, dict(ad, size_description="Medium"), status=201)["ad"]
    expect("…and cannot be edited to a bare number", "PATCH", f"/market/ads/{sized['id']}", owner.token, {"size_description": "52"}, status=400)
    expect("four photos are refused", "POST", "/market/ads", owner.token, dict(ad, photos=["https://cdn.example/p.jpg"] * 4), status=400)
    expect("a truck with no road is refused", "POST", "/market/ads", owner.token, dict(ad, road_accessible=False, truck_accessible=True, pickup_landmark="Gate"), status=400)
    expect("no road and no pickup point is refused", "POST", "/market/ads", owner.token, dict(ad, road_accessible=False, truck_accessible=False), status=400)
    acc = expect("road yes, truck no, with a pickup point and notes", "POST", "/market/ads", owner.token,
                 dict(ad, photos=["https://cdn.example/p.jpg"] * 3, road_accessible=True, truck_accessible=False,
                      pickup_landmark="Banyan tree on the village road", access_instructions="Narrow lane, tractors only"), status=201)["ad"]
    check("…stored as sent", acc["road_accessible"] is True and acc["truck_accessible"] is False
          and acc["pickup_landmark"] == "Banyan tree on the village road" and acc["access_instructions"] == "Narrow lane, tractors only", acc)
    expect("…switching road off keeps the pickup point, so it is allowed", "PATCH", f"/market/ads/{acc['id']}", owner.token, {"road_accessible": False})
    expect("…clearing the pickup point with no road is refused", "PATCH", f"/market/ads/{acc['id']}", owner.token, {"pickup_landmark": ""}, status=400)
    neem = expect("look up neem", "GET", "/plants/lookup?q=Azadirachta", owner.token)["results"][0]["plant"]
    expect("a wanted post cannot carry road or truck access", "POST", "/market/ads", owner.token,
           {"ad_type": "WANT", "plant_id": neem["id"], "plant_name": "Neem", "title": "Neem", "road_accessible": True}, status=400)

    # Sort and server-side paging.
    out = expect("browse sorted by plant name", "GET", "/market/ads?sort=name&per_page=50", rival.token)
    names = [x["plant_name"].lower() for x in out["ads"]]
    check("…A to Z", names == sorted(names), names)
    cheap = expect("an ad priced 10", "POST", "/market/ads", owner.token, dict(ad, title="Cheap", price_per_unit=10), status=201)["ad"]
    expect("…published", "POST", f"/market/ads/{cheap['id']}/publish", owner.token, {})
    ask = expect("an ad at price on request (0)", "POST", "/market/ads", owner.token, dict(ad, title="On request", price_per_unit=0), status=201)["ad"]
    expect("…published", "POST", f"/market/ads/{ask['id']}/publish", owner.token, {})
    for sort in ("price_asc", "price_desc"):
        out = expect(f"browse sorted {sort}", "GET", f"/market/ads?sort={sort}&per_page=50", rival.token)
        prices = [x.get("price_per_unit") or 0 for x in out["ads"]]
        priced = [p for p in prices if p > 0]
        check(f"…priced ads in order, price on request last", prices[:len(priced)] == sorted(priced, reverse=sort == "price_desc") and all(p == 0 for p in prices[len(priced):]), prices)
    p1 = expect("page 1 of 1-per-page", "GET", "/market/ads?per_page=1&page=1", rival.token)
    p2 = expect("page 2", "GET", "/market/ads?per_page=1&page=2", rival.token)
    check("…pages differ and share the total", p1["total"] == p2["total"] >= 2 and p1["ads"][0]["id"] != p2["ads"][0]["id"], (p1["total"], p2["total"]))

    # Local market: ads within the viewer's radius only (60 km default, 100 km cap).
    lat, lon = PORT_BLAIR
    near = expect("browse from the nursery's own address (no phone position)", "GET", "/market/ads?per_page=50", rival.token)
    hit = next((x for x in near["ads"] if x["id"] == cheap["id"]), None)
    check("…finds the ad, with its distance", hit is not None and hit.get("distance_km") is not None and hit["distance_km"] < 1, hit)
    delhi = expect("browse from Delhi", "GET", "/market/ads?per_page=50&near_lat=28.61&near_lon=77.21", rival.token)
    check("…a Port Blair ad is too far to show", cheap["id"] not in [x["id"] for x in delhi["ads"]], delhi["total"])
    away = f"near_lat={lat + 0.72}&near_lon={lon}"  # about 80 km north
    out = expect("browse from 80 km away, default radius", "GET", f"/market/ads?per_page=50&{away}", rival.token)
    check("…outside the 60 km default", cheap["id"] not in [x["id"] for x in out["ads"]], out["total"])
    out = expect("…widened to 100 km", "GET", f"/market/ads?per_page=50&{away}&radius_km=100", rival.token)
    check("…now it shows, about 80 km away", any(x["id"] == cheap["id"] and 70 < x["distance_km"] < 90 for x in out["ads"]), [x.get("distance_km") for x in out["ads"]])
    far = f"near_lat={lat + 1.35}&near_lon={lon}"  # about 150 km north
    out = expect("browse from 150 km asking for 500 km", "GET", f"/market/ads?per_page=50&{far}&radius_km=500", rival.token)
    check("…capped at 100 km, so not shown", cheap["id"] not in [x["id"] for x in out["ads"]], out["total"])
    out = expect("sort by nearest", "GET", f"/market/ads?per_page=50&sort=nearest&{away}&radius_km=100", rival.token)
    dists = [x["distance_km"] for x in out["ads"]]
    check("…closest first", dists == sorted(dists), dists)
    nowhere = approved_owner(boss, "No Location", coords=False)
    expect("a nursery with no coordinates and no phone position gets a clear error", "GET", "/market/ads", nowhere.token, status=422)
    d = expect("…it can still save a draft", "POST", "/market/ads", nowhere.token, dict(ad, title="Nowhere"), status=201)["ad"]
    expect("…but cannot publish an ad nobody could find", "POST", f"/market/ads/{d['id']}/publish", nowhere.token, {}, status=400)
    stuck = expect("admin lists ads with no location", "GET", "/market/admin/ads?no_location=true&per_page=100", boss.token)
    check("…the draft that could not be published is there", d["id"] in [x["id"] for x in stuck["ads"]]
          and all(not x["has_location"] for x in stuck["ads"]), stuck["pagination"])
    row = expect("…admin opens the 80 km test ad", "GET", f"/market/admin/ads?search={cheap['code']}", boss.token)["ads"][0]
    check("…with its location, size and pickup details", row["has_location"] and row.get("pickup_latitude") is not None
          and "pickup_address" in row, row)
    d = expect("…sending the phone's position fixes it", "POST", "/market/ads", nowhere.token,
               dict(ad, title="From the phone", pickup_latitude=lat, pickup_longitude=lon, pickup_location_source="gps_confirmed"), status=201)["ad"]
    expect("…publishes", "POST", f"/market/ads/{d['id']}/publish", nowhere.token, {})
    expect("…tidy up", "POST", f"/market/ads/{d['id']}/archive", nowhere.token, {})
    for leftover in (cheap, ask):  # keep test ads out of the dev market
        expect("…tidy up", "POST", f"/market/ads/{leftover['id']}/archive", owner.token, {})


def journey_wanted():
    """Wanted posts: a nursery asks for a plant and size (no price); others answer with 'I have this' and offer a price in the chat."""
    boss = admin()
    owner, _, buyer, _, plants = team(boss)
    rival = approved_owner(boss, "Supplier")
    want = {"ad_type": "WANT", "plant_id": plants[1], "plant_name": "Neem", "title": "Wanted: Neem",
            "size_description": "Sapling", "quantity": 300, "needed_by": time.strftime("%Y-%m-%d", time.localtime(time.time() + 20 * 86400)),
            "description": "For a highway project"}
    expect("a want with a price is refused", "POST", "/market/ads", owner.token, {**want, "price_per_unit": 50}, status=400)
    expect("a want without a catalog plant is refused", "POST", "/market/ads", owner.token, {k: v for k, v in want.items() if k != "plant_id"}, status=400)
    expect("a want needed in the past is refused", "POST", "/market/ads", owner.token, {**want, "needed_by": "2020-01-01"}, status=400)
    expect("needed_by on a sale ad is refused", "POST", "/market/ads", owner.token, {"plant_name": "Neem", "title": "x", "needed_by": want["needed_by"]}, status=400)
    expect("an unknown ad type is refused", "POST", "/market/ads", owner.token, {**want, "ad_type": "SWAP"}, status=400)
    w = expect("owner posts a want", "POST", "/market/ads", owner.token, want, status=201)["ad"]
    check("…WANT, no price, with the date", w["ad_type"] == "WANT" and w.get("price_per_unit") is None and w.get("needed_by", "").startswith(want["needed_by"]), w)
    expect("a want cannot gain a price later", "PATCH", f"/market/ads/{w['id']}", owner.token, {"price_per_unit": 10}, status=400)
    expect("owner publishes the want", "POST", f"/market/ads/{w['id']}/publish", owner.token, {})
    wanted = expect("a supplier browses wants", "GET", "/market/ads?type=WANT&per_page=50", rival.token)
    check("…sees it", w["id"] in [x["id"] for x in wanted["ads"]] and all(x["ad_type"] == "WANT" for x in wanted["ads"]), [x["id"] for x in wanted["ads"]])
    sale = expect("…and the for-sale list", "GET", "/market/ads?type=SELL&per_page=50", rival.token)
    check("…does not show wants", all(x["ad_type"] == "SELL" for x in sale["ads"]), [x["ad_type"] for x in sale["ads"]])
    expect("an unknown browse type is refused", "GET", "/market/ads?type=SWAP", rival.token, status=400)
    e = expect("the supplier answers 'I have this'", "POST", f"/market/ads/{w['id']}/enquiries", rival.token, {"message": "I have 300 neem saplings"}, status=201)["enquiry"]
    check("…a chat about the want", e["ad_type"] == "WANT", e)
    chat = f"/market/enquiries/{e['id']}"
    offer = expect("the supplier offers a price", "POST", f"{chat}/offers", rival.token, {"price_per_unit": 45, "quantity": 300}, status=201)["message"]
    deal = expect("the nursery that wanted it accepts", "POST", f"{chat}/offers/{offer['id']}/accept", owner.token, {})["enquiry"]
    check("…deal agreed at Rs 45 × 300", deal["status"] == "DEAL_AGREED" and float(deal["agreed_price_per_unit"]) == 45, deal)
    prof = expect("the wanting nursery's profile", "GET", f"/market/nurseries/{owner.nursery_id}", rival.token)["seller"]
    check("…lists the want", any(x["id"] == w["id"] and x["ad_type"] == "WANT" for x in prof["ads"]), [x["id"] for x in prof["ads"]])
    expect("buyers still cannot see the market", "GET", "/market/ads?type=WANT", buyer.token, status=403)


def journey_moderation():
    """Reported ads: a serious report hides the ad at once; the admin restores or removes it; removals are strikes that block posting."""
    boss = admin()
    owner, _, _, _, _ = team(boss)
    rival = approved_owner(boss, "Reporter")

    def live_ad(title):
        a = expect(f"owner posts '{title}'", "POST", "/market/ads", owner.token,
                   {"plant_name": "Rose", "title": title, "price_per_unit": 50, "photos": ["https://example.com/a.jpg"]}, status=201)["ad"]
        expect("…and publishes it", "POST", f"/market/ads/{a['id']}/publish", owner.token, {})
        return a["id"]

    def pending_report(ad_id):
        out = expect("admin opens the report queue", "GET", "/market/reports?status=PENDING&per_page=100", boss.token)
        return next(r for r in out["reports"] if r["ad_id"] == ad_id)

    def decide(report_id, decision, status=200):
        return expect(f"admin decides {decision}", "POST", f"/market/reports/{report_id}/review", boss.token, {"decision": decision}, status=status)

    # 1. A nudity report hides the ad before anyone reviews it; dismissing restores it.
    a1 = live_ad("Moderation one")
    expect("a rival reports nude images", "POST", f"/market/ads/{a1}/report", rival.token, {"reason": "EXPLICIT_CONTENT"})
    out = expect("the rival browses the market", "GET", "/market/ads?per_page=50", rival.token)
    check("…the reported ad is hidden at once", a1 not in [x["id"] for x in out["ads"]], [x["id"] for x in out["ads"]])
    expect("…and its page is gone for others", "GET", f"/market/ads/{a1}", rival.token, status=404)
    mine = expect("the owner opens the ad", "GET", f"/market/ads/{a1}", owner.token)["ad"]
    check("…sees it under review with a reason", mine["status"] == "SUSPENDED" and "review" in (mine.get("moderation_note") or ""), mine)
    expect("…cannot edit it", "PATCH", f"/market/ads/{a1}", owner.token, {"title": "sneaky fix"}, status=409)
    expect("…cannot republish it", "POST", f"/market/ads/{a1}/resume", owner.token, {}, status=400)
    expect("…cannot archive it to dodge review", "POST", f"/market/ads/{a1}/archive", owner.token, {}, status=400)
    rep = pending_report(a1)
    check("…the admin sees it flagged serious, with photos and strikes", rep["serious"] and rep["ad_photos"] and rep["ad_nursery_strikes"] == 0, rep)
    expect("a decision must be REMOVE or DISMISS", "POST", f"/market/reports/{rep['id']}/review", boss.token, {"decision": "MAYBE"}, status=400)
    decide(rep["id"], "DISMISS")
    mine = expect("the owner opens the ad again", "GET", f"/market/ads/{a1}", owner.token)["ad"]
    check("…it is live again and the note is gone", mine["status"] == "PUBLISHED" and not mine.get("moderation_note"), mine)
    decide(rep["id"], "REMOVE", status=409)

    # 2. A removal is strike one: a warning, no block.
    a2 = live_ad("Moderation two")
    expect("a rival reports a banned item", "POST", f"/market/ads/{a2}/report", rival.token, {"reason": "PROHIBITED"})
    res = decide(pending_report(a2)["id"], "REMOVE")
    check("…strike 1, no posting block", res["outcome"]["strikes"] == 1 and not res["outcome"].get("blocked_until"), res["outcome"])
    check("…the ad is REMOVED", res["report"]["ad_status"] == "REMOVED", res["report"])

    # 3. A non-serious report leaves the ad up; removing it is strike two: 30-day block.
    a3 = live_ad("Moderation three")
    expect("a rival reports a scam", "POST", f"/market/ads/{a3}/report", rival.token, {"reason": "SCAM"})
    still = expect("the owner opens the scam-reported ad", "GET", f"/market/ads/{a3}", owner.token)["ad"]
    check("…a scam report alone does not hide it", still["status"] == "PUBLISHED", still["status"])
    res = decide(pending_report(a3)["id"], "REMOVE")
    check("…strike 2 blocks posting", res["outcome"]["strikes"] == 2 and res["outcome"].get("blocked_until"), res["outcome"])
    expect("a blocked nursery cannot post a new ad", "POST", "/market/ads", owner.token, {"plant_name": "Rose", "title": "Blocked"}, status=403)
    expect("…nor bring a paused ad back", "POST", f"/market/ads/{a1}/pause", owner.token, {})
    expect("…resume is refused", "POST", f"/market/ads/{a1}/resume", owner.token, {}, status=403)
    expect("an unknown report reason is refused", "POST", f"/market/ads/{a1}/report", rival.token, {"reason": "UGLY"}, status=400)

    # 4. Admin acts directly from Admin → Local Market → All ads.
    expect("owners cannot list all ads", "GET", "/market/admin/ads", owner.token, status=403)
    expect("owners cannot moderate", "POST", f"/market/admin/ads/{a1}/moderate", owner.token, {"action": "REMOVE", "note": "x"}, status=403)
    out = expect("admin lists removed ads", "GET", "/market/admin/ads?status=REMOVED&per_page=100", boss.token)
    row = next(x for x in out["ads"] if x["id"] == a3)
    check("…with photos, poster, strikes and block", row["photos"] and row["posted_by_name"] and row["nursery_strikes"] == 2 and row.get("nursery_blocked_until"), row)
    expect("an unknown status filter is refused", "GET", "/market/admin/ads?status=GONE", boss.token, status=400)
    out = expect("admin searches by ad code", "GET", f"/market/admin/ads?search={row['code']}", boss.token)
    check("…finds it", [x["id"] for x in out["ads"]] == [a3], [x["id"] for x in out["ads"]])
    out = expect("admin lists blocked nurseries", "GET", "/market/admin/blocked-nurseries", boss.token)
    check("…this nursery is there with 2 strikes", any(b["nursery_id"] == owner.nursery_id and b["strikes"] == 2 for b in out["nurseries"]), out)
    expect("owners cannot list blocked nurseries", "GET", "/market/admin/blocked-nurseries", owner.token, status=403)
    expect("admin unblocks the nursery early", "POST", f"/market/admin/nurseries/{owner.nursery_id}/unblock", boss.token, {})
    out = expect("…and it leaves the blocked list", "GET", "/market/admin/blocked-nurseries", boss.token)
    check("…gone", all(b["nursery_id"] != owner.nursery_id for b in out["nurseries"]), out)
    a4 = live_ad("Moderation four")
    expect("hiding needs a note for the seller", "POST", f"/market/admin/ads/{a4}/moderate", boss.token, {"action": "HIDE"}, status=400)
    expect("an unknown action is refused", "POST", f"/market/admin/ads/{a4}/moderate", boss.token, {"action": "BAN", "note": "x"}, status=400)
    res = expect("admin hides a live ad nobody reported", "POST", f"/market/admin/ads/{a4}/moderate", boss.token, {"action": "HIDE", "note": "Photo looks copied"})
    check("…under review with the admin's note", res["ad"]["status"] == "SUSPENDED" and "Photo looks copied" in res["ad"]["moderation_note"], res["ad"])
    expect("…hiding twice is refused", "POST", f"/market/admin/ads/{a4}/moderate", boss.token, {"action": "HIDE", "note": "again"}, status=409)
    res = expect("admin restores it", "POST", f"/market/admin/ads/{a4}/moderate", boss.token, {"action": "RESTORE"})
    check("…live again, note cleared", res["ad"]["status"] == "PUBLISHED" and not res["ad"].get("moderation_note"), res["ad"])
    expect("…restoring a live ad is refused", "POST", f"/market/admin/ads/{a4}/moderate", boss.token, {"action": "RESTORE"}, status=409)
    res = expect("admin removes it directly", "POST", f"/market/admin/ads/{a4}/moderate", boss.token, {"action": "REMOVE", "note": "Fake stock"})
    check("…strike 3: a 90-day block", res["outcome"]["strikes"] == 3 and res["outcome"].get("blocked_until"), res["outcome"])
    expect("…removing twice is refused", "POST", f"/market/admin/ads/{a4}/moderate", boss.token, {"action": "REMOVE", "note": "again"}, status=409)
    expect("…and the nursery is blocked again", "POST", "/market/ads", owner.token, {"plant_name": "Rose", "title": "Blocked"}, status=403)


def journey_admin():
    """Admin-only routes refuse everyone else; admins cannot trade."""
    boss = admin()
    owner, manager, buyer, driver, _ = team(boss)
    for who, u in [("owner", owner), ("manager", manager), ("buyer", buyer), ("driver", driver)]:
        expect(f"{who} cannot open the admin dashboard", "GET", "/admin/dashboard", u.token, status=403)
        expect(f"{who} cannot list users", "GET", "/admin/users", u.token, status=403)
        expect(f"{who} cannot suspend a nursery", "PUT", f"/admin/nurseries/{owner.nursery_id}/status", u.token, {"status": "SUSPENDED", "reason": "x"}, status=403)
        expect(f"{who} cannot approve drivers", "POST", f"/drivers/{driver.id}/approve", u.token, {}, status=403)
        expect(f"{who} cannot see payment issues", "GET", "/payment-issues", u.token, status=403)
        expect(f"{who} cannot resolve a payment issue", "POST", "/payment-issues/1/resolve", u.token, {"note": "refunded by hand"}, status=403)
    issues = expect("admin sees the payment-issue queue", "GET", "/payment-issues?status=OPEN", boss.token)
    check("…as a list", isinstance((issues or {}).get("issues"), list), issues)
    expect("…an unknown status filter is refused", "GET", "/payment-issues?status=MAYBE", boss.token, status=400)
    expect("…resolving an unknown issue is not found", "POST", "/payment-issues/999999/resolve", boss.token, {"note": "refunded by hand"}, status=404)
    out = expect("admin dashboard", "GET", "/admin/dashboard", boss.token)
    check("…has numbers", bool(out), out)
    out = expect("admin finds a user by mobile", "GET", f"/admin/users?search={buyer.mobile}", boss.token)
    check("…found", buyer.mobile in json.dumps(out), out)
    expect("suspend without a reason is refused", "PUT", f"/admin/nurseries/{owner.nursery_id}/status", boss.token, {"status": "SUSPENDED"}, status=400)
    expect("an unknown user status is refused", "PUT", f"/admin/users/{buyer.id}/status", boss.token, {"status": "GHOST"}, status=400)
    expect("no token, no admin", "GET", "/admin/dashboard", None, status=401)

    # Admin tables: several values per filter (comma list) and sorting.
    both = expect("users with either of two roles", "GET", "/admin/users?role=DRIVER,BUYER&per_page=100", boss.token)
    roles = {r for u in both["users"] for r in u["roles"]}
    check("…only those roles appear", roles and roles <= {"DRIVER", "BUYER", "MANAGER", "NURSERY_OWNER"} and ("DRIVER" in roles or "BUYER" in roles), roles)
    one = expect("…one role alone still works", "GET", "/admin/users?role=DRIVER&per_page=1", boss.token)
    two = expect("…and two give at least as many", "GET", "/admin/users?role=DRIVER,BUYER&per_page=1", boss.token)
    check("…counts add up", two["pagination"]["total"] >= one["pagination"]["total"] > 0, (one["pagination"], two["pagination"]))
    out = expect("users sorted by mobile", "GET", "/admin/users?sort_by=mobile&sort_order=asc&per_page=20", boss.token)
    mobiles = [u["mobile"] for u in out["users"]]
    check("…in order", mobiles == sorted(mobiles), mobiles)
    out = expect("orders in two statuses", "GET", "/orders?order_status=PENDING,CANCELLED&per_page=100", boss.token)
    check("…only those statuses", all(o["order_status"] in ("PENDING", "CANCELLED") for o in out["orders"]), {o["order_status"] for o in out["orders"]})
    out = expect("payments by two methods", "GET", "/payments?payment_method=CASH,UPI&per_page=100", boss.token)
    check("…only those methods", all(p["payment_method"] in ("CASH", "UPI", None) for p in out["payments"]), {p["payment_method"] for p in out["payments"]})


def journey_redis():
    """Redis does its jobs: OTP rate limit, workspace cache invalidation, search cache generations expire, live GPS removed after delivery."""
    if not redis:
        check("Redis reachable (set REDIS_ADDR)", False)
        return
    m = new_mobile()
    redis.clear_rate_limits()
    codes = [call("POST", "/auth/send-otp", body={"mobile": m})[0] for _ in range(7)]
    check("OTP sends are rate-limited per mobile", codes[:5] == [200] * 5 and codes[-1] == 429, codes)
    check("…the limit key expires", 0 < int(redis.cmd("TTL", f"rl:otp:{m}") or 0) <= 600, redis.cmd("TTL", f"rl:otp:{m}"))
    redis.clear_rate_limits()

    boss = admin()
    owner, manager, buyer, driver, plants = team(boss)
    call("GET", "/me/workspaces", manager.token)
    check("workspaces are cached in Redis", bool(redis.keys(f"workspace:{manager.id}")), redis.keys("workspace:*")[:5])
    expect("manager leaves", "DELETE", "/nurseries/me/leave", manager.token, status=(200, 204))
    check("leaving drops the manager's cached workspaces", not redis.keys(f"workspace:{manager.id}"), redis.keys(f"workspace:{manager.id}"))

    expect("an order bumps the owner's search generation", "POST", "/orders", owner.token, {"seller_nursery_id": owner.nursery_id, "buyer_mobile": buyer.mobile,
           "items": [{"plant_id": plants[0], "quantity": 1, "unit_price": 10, "total_price": 10}]}, status=201)
    gen = f"cache:search:gen:orders:u{owner.id}"
    ttl = int(redis.cmd("TTL", gen) or -2)
    check("…and the generation key expires (no unbounded growth)", 0 < ttl <= 86400, ttl)
    check("lock keys never linger", not redis.keys("lock:*"), redis.keys("lock:*"))


def journey_top_items():
    """Top items: a nursery's showcase (not stock). Owner, its managers or admin edit; max 20; no duplicates; display order; notes + photos; sourcing network shows them."""
    boss = admin()
    owner, manager, buyer, driver, plants = team(boss)
    rival = approved_owner(boss, "Rival")
    nid = owner.nursery_id
    _, sizes = call("GET", "/plants/sizes")
    size_ids = [x["id"] for x in sizes["sizes"]]
    _, catalog = call("GET", "/plants?per_page=50", owner.token)
    plant_ids = [p["id"] for p in catalog["plants"]]

    out = expect("owner's top items", "GET", f"/nurseries/{nid}/top-items", owner.token)
    items = out["top_items"]
    check("team() listed two, in display order 0, 1", [i["display_order"] for i in items] == [0, 1], items)
    check("…code TOP-, price kept, no stock fields", items[0]["top_item_code"].startswith("TOP-") and items[0].get("unit_price") == 150
          and "available_quantity" not in items[0] and "inventory_status" not in items[0], items[0])
    check("…owner can edit", items[0]["capabilities"]["can_edit"] is True, items[0]["capabilities"])

    first = items[0]
    expect("same plant and size twice is refused", "POST", "/top-items", owner.token,
           {"nursery_id": nid, "plant_id": first["plant_id"], "size_id": first["size_id"]}, status=409)
    expect("negative price is refused", "POST", "/top-items", owner.token, {"nursery_id": nid, "plant_id": plant_ids[2], "size_id": size_ids[0], "unit_price": -5}, status=400)
    expect("display order outside 0–19 is refused", "POST", "/top-items", owner.token, {"nursery_id": nid, "plant_id": plant_ids[2], "size_id": size_ids[0], "display_order": 20}, status=400)
    expect("unknown nursery", "POST", "/top-items", boss.token, {"nursery_id": 999999999, "plant_id": plant_ids[2], "size_id": size_ids[0]}, status=404)
    expect("a rival owner cannot add to this nursery", "POST", "/top-items", rival.token, {"nursery_id": nid, "plant_id": plant_ids[2], "size_id": size_ids[0]}, status=403)
    out = expect("the nursery's manager adds one with notes and photos", "POST", "/top-items", manager.token,
                 {"nursery_id": nid, "plant_id": plant_ids[2], "size_id": size_ids[0], "quality_notes": "Grafted, 2 ft", "photos": ["https://cdn.example/p1.jpg"]}, status=201)
    check("…notes and photos saved, appended last (2)", out["top_item"]["quality_notes"] == "Grafted, 2 ft" and out["top_item"]["photos"] == ["https://cdn.example/p1.jpg"]
          and out["top_item"]["display_order"] == 2, out["top_item"])
    expect("manager removes it again", "DELETE", f"/top-items/{out['top_item']['id']}", manager.token)
    expect("six photos are refused", "POST", "/top-items", owner.token, {"nursery_id": nid, "plant_id": plant_ids[2], "size_id": size_ids[0], "photos": ["a"] * 6}, status=400)
    expect("a rival's manager-less owner cannot use this nursery", "POST", "/top-items", rival.token, {"nursery_id": nid, "plant_id": plant_ids[3], "size_id": size_ids[0]}, status=403)
    expect("a buyer cannot add top items", "POST", "/top-items", buyer.token, {"nursery_id": nid, "plant_id": plant_ids[2], "size_id": size_ids[0]}, status=403)
    expect("a rival owner cannot edit this item", "PUT", f"/top-items/{first['id']}", rival.token, {"plant_id": first["plant_id"], "size_id": first["size_id"], "unit_price": 1}, status=403)
    expect("a rival owner cannot delete this item", "DELETE", f"/top-items/{first['id']}", rival.token, status=403)
    out = expect("buyer reads the nursery's top items", "GET", f"/nurseries/{nid}/top-items", buyer.token)
    check("…read-only for the buyer", all(not i["capabilities"]["can_edit"] for i in out["top_items"]), out)
    expect("no sign-in, no top items", "GET", "/top-items", None, status=401)

    out = expect("owner changes price and moves it last", "PUT", f"/top-items/{first['id']}", owner.token,
                 {"plant_id": first["plant_id"], "size_id": first["size_id"], "unit_price": 175.5, "display_order": 5})
    check("…saved", out["top_item"]["unit_price"] == 175.5 and out["top_item"]["display_order"] == 5, out["top_item"])
    out = expect("owner clears the price", "PUT", f"/top-items/{first['id']}", owner.token, {"plant_id": first["plant_id"], "size_id": first["size_id"]})
    check("…no price, order kept", "unit_price" not in out["top_item"] and out["top_item"]["display_order"] == 5, out["top_item"])
    expect("a stale If-Match is refused", "PUT", f"/top-items/{first['id']}", owner.token, {"plant_id": first["plant_id"], "size_id": first["size_id"]},
           status=409, headers={"If-Match": "1"})

    # Fill to the limit of 20
    pairs = [(p, z) for p in plant_ids for z in size_ids if (p, z) not in {(i["plant_id"], i["size_id"]) for i in items}]
    added = 0
    for p, z in pairs[:18]:
        code, _ = call("POST", "/top-items", owner.token, {"nursery_id": nid, "plant_id": p, "size_id": z})
        added += code == 201
    check("owner fills the list to 20", added == 18, added)
    p, z = pairs[18]
    out = expect("the 21st top item is refused", "POST", "/top-items", owner.token, {"nursery_id": nid, "plant_id": p, "size_id": z}, status=409)
    check("…code top_items_limit", err_code(out) == "top_items_limit", out)
    out = expect("admin cannot pass the limit either", "POST", "/top-items", boss.token, {"nursery_id": nid, "plant_id": p, "size_id": z}, status=409)
    expect("owner removes one", "DELETE", f"/top-items/{first['id']}", owner.token)
    expect("…and can add again", "POST", "/top-items", owner.token, {"nursery_id": nid, "plant_id": p, "size_id": z}, status=201)
    expect("a removed item is gone", "GET", f"/top-items/{first['id']}", owner.token, status=404)

    # Admin view across nurseries
    out = expect("admin lists top items of all nurseries", "GET", "/top-items?per_page=100", boss.token)
    nurseries = {i["nursery_id"] for i in out["top_items"]}
    check("…several nurseries, paginated", len(nurseries) >= 2 and out["pagination"]["total"] >= 20, out["pagination"])
    out = expect("admin filters one nursery", "GET", f"/top-items?nursery_id={rival.nursery_id}", boss.token)
    check("…only that nursery", all(i["nursery_id"] == rival.nursery_id for i in out["top_items"]), out)
    # Filtered to this nursery: every journey run adds more neem nurseries, so page 1 alone is not stable.
    out = expect("which nurseries list neem", "GET", f"/plants/{plants[1]}/top-items?nursery_id={nid}", buyer.token)
    check("…includes this nursery", nid in [i["nursery_id"] for i in out["top_items"]], out)
    expect("owner joins the sourcing network", "POST", f"/nurseries/{nid}/sourcing-membership", owner.token,
           {"road_accessible": True, "lorry_accessible": True, "contact_visible": True, "service_radius_km": 50}, status=(200, 201))
    out = expect("rival finds the nursery in the network", "GET", f"/sourcing-network/nurseries/{nid}", rival.token)
    shown = out.get("nursery", out).get("top_items", [])
    check("…with its top items (at most 20)", 0 < len(shown) <= 20 and all("plant_name" in x and "photos" in x for x in shown), shown[:2])
    out = expect("owner dashboard counts top items", "GET", "/me/owner-dashboard", owner.token)
    check("…20", out.get("top_items", out.get("dashboard", {}).get("top_items", {})).get("total") == 20, out)


def journey_roles():
    """Role changes on one account: managers leave or are removed (work goes back to the owner), drivers stop driving, owners close their nursery; each can take another role later."""
    boss = admin()
    owner = approved_owner(boss, "Roles Owner")
    other = approved_owner(boss, "Roles Other")
    _, plants = call("GET", "/plants?per_page=1")
    plant = (plants.get("plants") or plants.get("data") or [{}])[0].get("id", 1)

    # Manager leaves: their assigned quotation goes back to the owner.
    mgr = User(first="Leaver", activity="MANAGER")
    expect("manager accepts invite", "POST", f"/invites/{invite(owner, 'MANAGER_INVITE', mgr)}/accept", mgr.token, {})
    mgr.refresh()
    q = expect("owner drafts a quotation", "POST", "/quotations", owner.token, {"quotation_type": "INTERNAL", "nursery_id": owner.nursery_id,
               "items": [{"plant_id": plant, "quantity": 1, "unit_price": 10, "total_price": 10}]}, status=201)["quotation"]
    expect("owner assigns it to the manager", "POST", f"/quotations/{q['id']}/assign-manager", owner.token, {"manager_user_id": mgr.id})
    expect("manager leaves", "DELETE", "/nurseries/me/leave", mgr.token)
    expect("owner can edit again after the assigned manager left", "PUT", f"/quotations/{q['id']}", owner.token,
           {"quotation_type": "INTERNAL", "nursery_id": owner.nursery_id, "notes": "back to owner",
            "items": [{"plant_id": plant, "quantity": 2, "unit_price": 10, "total_price": 20}]})
    mgr.login()
    check("ex-manager is a buyer again", mgr.roles() == {"BUYER"}, mgr.roles())
    expect("ex-manager can't open the old nursery's quotation", "GET", f"/quotations/{q['id']}", mgr.token, status=(403, 404))
    expect("ex-manager joins another nursery", "POST", f"/invites/{invite(other, 'MANAGER_INVITE', mgr)}/accept", mgr.token, {})

    # Approval reaches a signed-in applicant at once, and admins can find pending applications.
    applicant = User(first="Applicant", activity="DRIVER")
    app_ = expect("applicant applies", "POST", "/drivers/apply", applicant.token,
                  {"licence_number": "TS09" + applicant.mobile, "vehicle_number": "TS09" + applicant.mobile[-4:], "vehicle_type": "VAN"}, status=(200, 201))["driver"]
    expect("app loads (and caches) workspaces before approval", "GET", "/me/workspaces", applicant.token)
    pending = expect("admin lists pending applications", "GET", "/drivers?approval_status=PENDING&per_page=100", boss.token)
    check("…only pending, including the applicant", all(d.get("approval_status") == "PENDING" for d in pending.get("drivers", [])) and any(d["id"] == app_["id"] for d in pending.get("drivers", [])), pending.get("pagination"))
    expect("admin approves", "POST", f"/drivers/{app_['id']}/approve", boss.token, {})
    ws = expect("workspaces right after approval", "GET", "/me/workspaces", applicant.token)
    check("…include DRIVER (cache cleared for the right user)", any(w.get("type") == "DRIVER" for w in (ws if isinstance(ws, list) else ws.get("workspaces", []))), ws)

    # Optional licence photo: only a file in our private bucket; admins open it by a short-lived link.
    lic = User(first="Licence", activity="DRIVER")
    body = {"licence_number": "TS09" + lic.mobile, "vehicle_number": "TS09" + lic.mobile[-4:], "vehicle_type": "VAN"}
    expect("a licence photo link from elsewhere is refused", "POST", "/drivers/apply", lic.token,
           dict(body, licence_photo_url="https://example.com/licence.png"), status=400)
    pre = expect("presign a licence photo upload", "POST", "/storage/presign", lic.token,
                 {"bucket": "attachments", "file_name": "licence.png", "content_type": "image/png", "size": len(SELFIE_PNG)})
    put = urllib.request.Request(pre["upload_url"], data=SELFIE_PNG, method="PUT", headers={"Content-Type": "image/png"})
    with urllib.request.urlopen(put, timeout=30) as r:
        check("…uploaded to storage", r.status in (200, 204), r.status)
    check("…the private key carries its uploader", pre["key"].startswith(f"u{lic.id}/"), pre["key"])
    peeker = User(first="Other", activity="DRIVER")
    expect("someone else's private file cannot be used as a licence photo", "POST", "/drivers/apply", peeker.token,
           {"licence_number": "TS09" + peeker.mobile, "vehicle_number": "TS09" + peeker.mobile[-4:], "vehicle_type": "VAN",
            "licence_photo_url": pre["file_url"]}, status=400)
    drv_lic = expect("apply with the licence photo", "POST", "/drivers/apply", lic.token,
                     dict(body, licence_photo_url=pre["file_url"]), status=(200, 201))["driver"]
    check("…stored", drv_lic.get("licence_photo_url") == pre["file_url"], drv_lic)
    out = expect("admin opens the application", "GET", f"/drivers/{drv_lic['id']}", boss.token)
    link = out.get("driver", {}).get("licence_photo_link")
    check("…with a link to the photo", bool(link), out)
    if link:
        with urllib.request.urlopen(link, timeout=30) as r:
            check("…the link serves the photo", r.read() == SELFIE_PNG)
    try:
        urllib.request.urlopen(pre["file_url"], timeout=30)
        check("the plain file URL is not public", False)
    except urllib.error.HTTPError as e:
        check("the plain file URL is not public", e.code == 403, e.code)
    expect("another user cannot see the application", "GET", f"/drivers/{drv_lic['id']}", applicant.token, status=403)

    # Driver stops driving, then can own a nursery; re-applying means review again.
    drv = approved_driver(boss, owner)
    expect("driver can't register a nursery", "POST", "/nurseries", drv.token, {"name": "Drv Farm", "mobile": drv.mobile, "address_line1": "x", "city": "Hyderabad"}, status=409)
    expect("driver stops driving", "DELETE", "/drivers/me", drv.token)
    drv.login()
    check("ex-driver is a buyer again", drv.roles() == {"BUYER"}, drv.roles())
    expect("ex-driver applies again", "POST", "/drivers/apply", drv.token, {"licence_number": "TS09" + drv.mobile, "vehicle_number": "TS09" + drv.mobile[-4:], "vehicle_type": "VAN"}, status=(200, 201))
    out = expect("…back to review", "GET", "/drivers/me", drv.token)
    check("…approval PENDING", out.get("driver", {}).get("approval_status") == "PENDING", out)
    expect("stopping again (pending profile)", "DELETE", "/drivers/me", drv.token)
    drv.login()
    expect("…and can register a nursery", "POST", "/nurseries", drv.token, {"name": f"Ex Driver {drv.mobile[-4:]}", "mobile": drv.mobile, "address_line1": "x", "city": "Hyderabad"}, status=201)

    # Owner closes the nursery: blocked while work is open, then everyone is released.
    closer = approved_owner(boss, "Closer")
    staff = User(first="Staff", activity="MANAGER")
    expect("staff joins", "POST", f"/invites/{invite(closer, 'MANAGER_INVITE', staff)}/accept", staff.token, {})
    o = expect("open order", "POST", "/orders", closer.token, {"seller_nursery_id": closer.nursery_id, "buyer_mobile": new_mobile(),
               "delivery": {"contact_name": "B", "address_line1": "x", "city": "Hyderabad"},
               "items": [{"plant_id": plant, "quantity": 1, "unit_price": 10, "total_price": 10}]}, status=201)["order"]
    expect("closing with an open order is refused", "DELETE", f"/nurseries/{closer.nursery_id}", closer.token, status=409)
    expect("cancel it", "POST", f"/orders/{o['id']}/cancel", closer.token, {"reason": "closing"})
    expect("owner closes the nursery", "DELETE", f"/nurseries/{closer.nursery_id}", closer.token)
    closer.login()
    staff.login()
    check("ex-owner is a buyer", closer.roles() == {"BUYER"}, closer.roles())
    check("its manager is a buyer", staff.roles() == {"BUYER"}, staff.roles())
    expect("ex-owner becomes a manager elsewhere", "POST", f"/invites/{invite(other, 'MANAGER_INVITE', closer)}/accept", closer.token, {})
    closer.refresh()
    check("…holding only MANAGER", closer.roles() == {"MANAGER"}, closer.roles())


def journey_access():
    """Subscription and suspension changes reach signed-in owners and managers at once; pause is admin-only; owners can't loop the free trial; suspensions say why."""
    boss = admin()
    owner = approved_owner(boss, "Access Owner")
    mgr = User(first="Access", activity="MANAGER")
    expect("manager joins", "POST", f"/invites/{invite(owner, 'MANAGER_INVITE', mgr)}/accept", mgr.token, {})
    mgr.refresh()
    _, plans = call("GET", "/subscription-plans")
    plan_list = plans.get("plans", plans if isinstance(plans, list) else [])
    plan_ids = {p.get("plan_code"): p.get("id") for p in plan_list}
    by_code = {p.get("plan_code"): p for p in plan_list}
    # Monthly base prices; quarterly / half-yearly / yearly are 5 / 10 / 15 % off.
    want = {"BASIC": ("Silver", 299, 1, [299, 852.15, 1614.6, 3049.8]), "GROWTH": ("Gold", 499, 5, [499, 1422.15, 2694.6, 5089.8]),
            "ENTERPRISE": ("Platinum", 999, 10, [999, 2847.15, 5394.6, 10189.8])}
    for code_, (name, monthly, managers, amounts) in want.items():
        p = by_code.get(code_, {})
        f = p.get("features") or {}
        cycles = [(c.get("billing_cycle"), c.get("amount")) for c in p.get("cycle_prices") or []]
        check(f"plan {name}: ₹{monthly}/month, {managers} manager(s), all four cycles priced",
              p.get("plan_name") == name and p.get("monthly_price") == monthly and p.get("max_managers") == managers
              and cycles == list(zip(["MONTHLY", "QUARTERLY", "SIX_MONTH", "YEARLY"], amounts)), p)
        gold_q = next((c for c in p.get("cycle_prices") or [] if c.get("billing_cycle") == "QUARTERLY"), {})
        check(f"…{name} quarterly shows 18% GST and the total", gold_q.get("total_amount") == round(gold_q.get("amount", 0) + gold_q.get("gst_amount", -1), 2)
              and abs(gold_q.get("gst_amount", 0) - round(gold_q.get("amount", 0) * 0.18, 2)) <= 0.01, gold_q)
        check(f"…{name}: unlimited orders and quotations, local market ads",
              f.get("unlimited_orders") is True and f.get("unlimited_quotations") is True and f.get("local_market_ads") is True, f)
    sub = expect("owner's trial", "GET", "/subscriptions/me", owner.token)["subscriptions"][0]
    q = {"quotation_type": "INTERNAL", "nursery_id": owner.nursery_id, "items": [{"plant_id": 1, "quantity": 1, "unit_price": 10, "total_price": 10}]}
    time.sleep(1.1)  # tokens issued in an earlier second than the change

    expect("admin pauses the subscription", "PUT", f"/subscriptions/{sub['id']}/status", boss.token, {"subscription_status": "PAUSED"})
    expect("owner's existing token must refresh", "POST", "/quotations", owner.token, q, status=401)
    expect("manager's existing token must refresh", "GET", "/quotations", mgr.token, status=401)
    owner.refresh(); mgr.refresh()
    expect("paused: owner can't create", "POST", "/quotations", owner.token, q, status=402)
    expect("paused: manager can't create", "POST", "/quotations", mgr.token, q, status=402)
    ws = [w for w in call("GET", "/me/workspaces", mgr.token)[1] if w.get("type") == "MANAGER_NURSERY"]
    check("…the manager's workspace says the nursery's plan is not active", ws and ws[0].get("subscription_active") is False, ws)
    expect("owner can't pay past an admin pause", "POST", f"/subscriptions/{sub['id']}/renew", owner.token,
           {"plan_id": plan_ids.get("GROWTH"), "billing_cycle": "YEARLY", "payment_method": "UPI"}, status=409)
    expect("admin resumes", "PUT", f"/subscriptions/{sub['id']}/status", boss.token, {"subscription_status": "ACTIVE"})
    ws = [w for w in call("GET", "/me/workspaces", mgr.token)[1] if w.get("type") == "MANAGER_NURSERY"]
    check("…active again for the manager", ws and ws[0].get("subscription_active") is True, ws)
    time.sleep(1.1)
    expect("owner renewing the free trial is refused", "POST", f"/subscriptions/{sub['id']}/renew", owner.refresh() and owner.token,
           {"billing_cycle": "SIX_MONTH", "payment_method": "UPI"}, status=400)
    # Razorpay on or off: the suite checks whichever mode this API runs in.
    upgrade = {"plan_id": plan_ids.get("GROWTH"), "billing_cycle": "YEARLY", "payment_method": "UPI"}
    code, po = call("POST", "/subscriptions/payment-order", owner.token,
                    {"subscription_id": sub["id"], "plan_id": plan_ids.get("GROWTH"), "billing_cycle": "YEARLY"})
    expect("only admins refund", "POST", f"/subscriptions/{sub['id']}/payments/1/refund", owner.token,
           {"reason": "please refund"}, status=403)
    if code == 503:
        # No keys: payments unavailable, the app falls back to mock payments.
        check("no Razorpay keys: payment order says payments unavailable", True)
        out = expect("owner upgrades to GROWTH", "POST", f"/subscriptions/{sub['id']}/renew", owner.token, upgrade)
    else:
        # Keys set (no money moves: the order is only created, never paid).
        order = po.get("payment_order", {}) if isinstance(po, dict) else {}
        check("Razorpay on: the server creates and prices the order (₹499 × 12 − 15% = ₹5,089.80 + 18% GST = ₹6,005.96, in paise)",
              code in (200, 201) and order.get("order_id", "").startswith("order_") and order.get("amount") == 600596, po)
        _, q_po = call("POST", "/subscriptions/payment-order", owner.token,
                       {"subscription_id": sub["id"], "plan_id": plan_ids.get("BASIC"), "billing_cycle": "QUARTERLY"})
        check("…Silver quarterly is ₹299 × 3 − 5% = ₹852.15 + GST = ₹1,005.54", (q_po.get("payment_order") or {}).get("amount") == 100554, q_po)
        expect("…an unknown cycle is refused", "POST", "/subscriptions/payment-order", owner.token,
               {"subscription_id": sub["id"], "plan_id": plan_ids.get("BASIC"), "billing_cycle": "WEEKLY"}, status=400)
        expect("Razorpay on: an owner cannot renew without a verified payment", "POST", f"/subscriptions/{sub['id']}/renew", owner.token, upgrade, status=400)
        out = expect("admin records an offline payment to upgrade to GROWTH", "POST", f"/subscriptions/{sub['id']}/renew", boss.token,
                     dict(upgrade, payment_method="CASH"))
    check("…plan switched", out.get("subscription", {}).get("plan_code") == "GROWTH", out)
    expect("renewing again while paid > 1 year ahead is refused", "POST", f"/subscriptions/{sub['id']}/renew", boss.token,
           dict(upgrade, payment_method="CASH"), status=409)
    owner.refresh()
    expect("upgraded owner creates", "POST", "/quotations", owner.token, q, status=201)

    # Suspensions carry the reason and say whether it's the account or the nursery.
    time.sleep(1.1)
    expect("admin suspends the nursery", "PUT", f"/admin/nurseries/{owner.nursery_id}/status", boss.token, {"status": "SUSPENDED", "reason": "GST pending"})
    code, out = call("GET", "/quotations", mgr.token)
    err = out.get("error", {}) if isinstance(out, dict) else {}
    check("manager is told the nursery is suspended, with the reason", code == 403 and err.get("code") == "NURSERY_SUSPENDED" and err.get("reason") == "GST pending", out)
    expect("admin reactivates", "PUT", f"/admin/nurseries/{owner.nursery_id}/status", boss.token, {"status": "ACTIVE"})
    mgr.refresh()  # as the app does after the 401 (claims changed during the test)
    expect("manager works again", "GET", "/quotations", mgr.token)
    buyer = User(first="Suspendee", activity="CUSTOMER")
    expect("admin suspends a user", "PUT", f"/admin/users/{buyer.id}/status", boss.token, {"status": "SUSPENDED", "reason": "fake orders"})
    code, out = call("GET", "/orders", buyer.token)
    check("…told why", code == 403 and out.get("error", {}).get("reason") == "fake orders", out)
    expect("admin reactivates the user", "PUT", f"/admin/users/{buyer.id}/status", boss.token, {"status": "ACTIVE"})
    code, _ = call("POST", "/auth/refresh-token", body={"refresh_token": buyer.refresh_token})
    check("…old sessions were signed out (must sign in again)", code == 401, code)


def journey_plant_suggestions():
    """Community plant suggestions: anyone suggests a missing plant; known names and repeats are ignored; only admins review; the suggester is told."""
    boss = admin()
    u = User(first="Sita", activity="CUSTOMER")
    other = User(first="Gopi", activity="CUSTOMER")
    tag = str(random.randint(10**5, 10**6))
    name = f"Kanakambaram {tag}"

    out = expect("suggest a missing plant", "POST", "/plants/suggestions", u.token, {"name": f"  {name} ", "notes": "orange flower"}, status=201)
    check("…SUBMITTED, PENDING, name tidied", out["result"] == "SUBMITTED" and out["suggestion"]["status"] == "PENDING"
          and out["suggestion"]["name"] == name, out)
    sid = out["suggestion"]["id"]
    out = expect("someone else suggests the same name", "POST", "/plants/suggestions", other.token, {"name": name.upper()})
    check("…ignored: ALREADY_SUGGESTED", out["result"] == "ALREADY_SUGGESTED", out)
    _, catalog = call("GET", "/plants?per_page=50", u.token)
    known = next(p for p in catalog["plants"] if p.get("common_name"))  # linking must not rename it
    out = expect("suggest a plant the catalog has", "POST", "/plants/suggestions", u.token, {"name": known["scientific_name"].lower()})
    check("…ignored: EXISTS with the plant", out["result"] == "EXISTS" and out["plant"]["id"] == known["id"], out)
    expect("a one-letter name is refused", "POST", "/plants/suggestions", u.token, {"name": "x"}, status=400)

    expect("a user cannot see the review queue", "GET", "/plants/suggestions", u.token, status=403)
    expect("a user cannot review", "POST", f"/plants/suggestions/{sid}/review", u.token, {"action": "LINKED", "plant_id": known["id"]}, status=403)
    out = expect("admin sees the queue", "GET", "/plants/suggestions?status=PENDING&per_page=100", boss.token)
    check("…with the suggestion and who sent it", any(x["id"] == sid and x["suggested_by_mobile"] == u.mobile for x in out["suggestions"]), out)
    expect("linking needs a plant", "POST", f"/plants/suggestions/{sid}/review", boss.token, {"action": "LINKED"}, status=400)
    out = expect("admin links it to an existing plant", "POST", f"/plants/suggestions/{sid}/review", boss.token, {"action": "LINKED", "plant_id": known["id"]})
    check("…LINKED to that plant", out["suggestion"]["status"] == "LINKED" and out["suggestion"]["plant_id"] == known["id"], out)
    _, after = call("GET", f"/plants/{known['id']}", u.token)
    check("…its existing common name is kept", after["plant"]["common_name"] == known["common_name"], after)
    expect("a reviewed suggestion cannot be reviewed again", "POST", f"/plants/suggestions/{sid}/review", boss.token, {"action": "REJECTED", "note": "x"}, status=409)
    out = expect("the suggester's notifications", "GET", "/notifications", u.token)
    check("…told it was added", "Thanks for your plant suggestion" in json.dumps(out), out)

    out = expect("suggest another", "POST", "/plants/suggestions", u.token, {"name": f"Not a plant {tag}"}, status=201)
    rid = out["suggestion"]["id"]
    expect("rejecting needs a reason", "POST", f"/plants/suggestions/{rid}/review", boss.token, {"action": "REJECTED"}, status=400)
    out = expect("admin rejects with a reason", "POST", f"/plants/suggestions/{rid}/review", boss.token, {"action": "REJECTED", "note": "Not a plant name"})
    check("…REJECTED with the reason", out["suggestion"]["status"] == "REJECTED" and out["suggestion"]["review_note"] == "Not a plant name", out)

    # 10 a day per user (two used above)
    codes = [call("POST", "/plants/suggestions", u.token, {"name": f"Plant {tag} {i}"})[0] for i in range(9)]
    check("the 11th suggestion in a day is refused (429)", codes[:8] == [201] * 8 and codes[8] == 429, codes)


def journey_storage():
    """Direct uploads: only the buckets apps upload to, only allowed file types, and the signed type is enforced."""
    u = User(first="Uploader", activity="CUSTOMER")
    expect("server-only bucket refused", "POST", "/storage/presign", u.token,
           {"bucket": "plant-images", "file_name": "a.jpg", "content_type": "image/jpeg"}, status=400)
    expect("a web page in a public bucket refused", "POST", "/storage/presign", u.token,
           {"bucket": "market-ads", "file_name": "a.html", "content_type": "text/html"}, status=400)
    expect("SVG in a public bucket refused", "POST", "/storage/presign", u.token,
           {"bucket": "market-ads", "file_name": "a.svg", "content_type": "image/svg+xml"}, status=400)
    expect("an upload without its size is refused", "POST", "/storage/presign", u.token,
           {"bucket": "market-ads", "file_name": "a.jpg", "content_type": "image/jpeg"}, status=400)
    out = expect("a photo over 10 MB is refused", "POST", "/storage/presign", u.token,
                 {"bucket": "market-ads", "file_name": "a.jpg", "content_type": "image/jpeg", "size": 10 * 1024 * 1024 + 1}, status=400)
    check("…file_too_large", err_code(out) == "file_too_large", out)
    expect("an attachment up to 20 MB is allowed", "POST", "/storage/presign", u.token,
           {"bucket": "attachments", "file_name": "a.pdf", "content_type": "application/pdf", "size": 20 * 1024 * 1024})
    pre = expect("photo upload allowed", "POST", "/storage/presign", u.token,
                 {"bucket": "market-ads", "file_name": "evil.html", "content_type": "image/jpeg", "size": len(SELFIE_PNG)})
    check("…extension comes from the type, not the file name", pre["key"].endswith(".jpg"), pre["key"])

    def put(content_type, data=SELFIE_PNG):
        req = urllib.request.Request(pre["upload_url"], data=data, method="PUT", headers={"Content-Type": content_type})
        try:
            with urllib.request.urlopen(req, timeout=30) as r:
                return r.status
        except urllib.error.HTTPError as e:
            return e.code
    check("uploading as a different type is refused", put("text/html") == 403)
    check("uploading a bigger file than declared is refused", put("image/jpeg", SELFIE_PNG * 50) == 403)
    check("uploading as the signed type works", put("image/jpeg") in (200, 204))
    with urllib.request.urlopen(pre["file_url"], timeout=30) as r:
        check("…served as an image", r.headers.get("Content-Type") == "image/jpeg", r.headers.get("Content-Type"))
    upload_selfie("a web page posing as a profile photo is refused", u, data=b"<html><script>alert(1)</script></html>", want=400)


def journey_reports():
    """Reports: each role sees only its slice; summaries (sales vs open vs paid, cancelled left out); paging; CSV masked and formula-safe; validation."""
    boss = admin()
    owner, manager, buyer, driver, plants = team(boss)
    rival = approved_owner(boss, "Rival")
    nid = owner.nursery_id
    today = time.strftime("%Y-%m-%d", time.gmtime(time.time() + 5.5 * 3600))  # India day
    rng = f"from={today}&to={today}"

    def order(name, qty):
        return expect("owner creates an order", "POST", "/orders", owner.token,
                      {"seller_nursery_id": nid, "buyer_mobile": buyer.mobile, "buyer_name": name,
                       "items": [{"plant_id": plants[0], "quantity": qty, "unit_price": 100, "total_price": qty * 100}]}, status=201)["order"]

    o1 = order("Ravi", 10)                     # 1000, paid in full
    o2 = order("Ravi", 5)                      # 500, cancelled
    o3 = order("Ravi", 3)                      # 300, manager's, payment refunded
    expect("cancel one order", "POST", f"/orders/{o2['id']}/cancel", owner.token, {"reason": "journey"})
    expect("assign one to the manager", "POST", f"/orders/{o3['id']}/assign-manager", owner.token, {"manager_user_id": manager.id})
    pay = lambda o, amt: expect("record a payment", "POST", "/payments/manual", owner.token,
                                {"payment_for": "ORDER", "order_id": o["id"], "amount": amt, "payment_method": "UPI", "payment_status": "SUCCESS"}, status=201)["payment"]
    pay(o1, 1000)
    refunded = pay(o3, 300)
    expect("refund it", "PUT", f"/payments/{refunded['id']}/status", owner.token, {"payment_status": "REFUNDED"})
    quote = {"quotation_type": "CUSTOMER", "nursery_id": nid, "recipient_name": '=HYPERLINK("http://x")', "recipient_mobile": buyer.mobile,
             "items": [{"plant_id": plants[0], "quantity": 2, "unit_price": 100, "total_price": 200}]}
    q = expect("a quotation", "POST", "/quotations", owner.token, quote, status=201)["quotation"]
    expect("…sent", "POST", f"/quotations/{q['id']}/send", owner.token, {})
    expect("a draft (never sent)", "POST", "/quotations", owner.token, quote, status=201)

    out = expect("owner: orders report", "GET", f"/reports/orders?{rng}", owner.token)
    s = out.get("summary", {})
    check("…3 orders, 1 cancelled, none delivered", s.get("total") == 3 and s.get("cancelled") == 1 and s.get("delivered", 0) == 0, s)
    check("…sales counts delivered orders only (0); open value leaves the cancelled one out (1300)",
          s.get("sales", 0) == 0 and s.get("open_value") == 1300, s)
    check("…paid is successful payments only, the refund is not money in (1000)", s.get("paid") == 1000, s)
    row = next((r for r in out.get("rows", []) if r["reference"] == o3["order_code"]), {})
    check("…rows show the customer (account name) with the mobile masked", row.get("party") == "Buyer Tester" and row.get("mobile") == buyer.mobile[:2] + "xxxxx" + buyer.mobile[-3:], row)
    page = expect("…page 2 of 2 (per_page=2)", "GET", f"/reports/orders?{rng}&per_page=2&page=2", owner.token)
    check("…one row, 2 pages", len(page.get("rows", [])) == 1 and page.get("pagination", {}).get("total_pages") == 2, page.get("pagination"))

    out = expect("owner: quotations report", "GET", f"/reports/quotations?{rng}", owner.token)
    check("…the sent quotation is pending; the draft is not in the report", out["summary"].get("total") == 1 and out["summary"].get("pending") == 1, out["summary"])
    out = expect("owner: deliveries report", "GET", f"/reports/deliveries?{rng}", owner.token)
    check("…no trips yet", out["summary"].get("total") == 0 and out.get("rows") == [], out)

    out = expect("manager: orders report", "GET", f"/reports/orders?{rng}", manager.token)
    check("…only the order assigned to them", [r["reference"] for r in out.get("rows", [])] == [o3["order_code"]], out.get("rows"))
    expect("assign the quotation to the manager", "POST", f"/quotations/{q['id']}/assign-manager", owner.token, {"manager_user_id": manager.id})
    out = expect("manager: quotations report", "GET", f"/reports/quotations?{rng}", manager.token)
    check("…their quotation, without the customer's name or mobile (as in the quotations list)",
          len(out.get("rows", [])) == 1 and not out["rows"][0].get("party") and "mobile" not in out["rows"][0], out.get("rows"))
    expect("the manager's order gets an address", "PUT", f"/orders/{o3['id']}/delivery", owner.token,
           {"contact_name": "Ravi", "contact_mobile": buyer.mobile, "address_line1": "12 Lake Road", "city": "Hyderabad", "state": "Telangana", "postal_code": "500032"})
    for step in ("confirm", "start-loading", "complete-loading"):
        expect(step, "POST", f"/orders/{o3['id']}/{step}", owner.token, LOADED_OK if step == "complete-loading" else {})
    trip = expect("the owner sends it out", "POST", "/dispatches", owner.token, {"order_id": o3["id"]}, status=201)["dispatch"]
    out = expect("manager: deliveries report", "GET", f"/reports/deliveries?{rng}", manager.token)
    check("…the trip of their order, though the owner created it", [r["reference"] for r in out.get("rows", [])] == [trip["dispatch_code"]], out.get("rows"))
    out = expect("customer: orders report", "GET", f"/reports/orders?{rng}", buyer.token)
    check("…their 3 purchases, named by nursery, no phone column", out["summary"].get("total") == 3 and all(r.get("party", "").startswith("Journey Nursery") and "mobile" not in r for r in out["rows"]), out.get("rows"))
    out = expect("customer: quotations report", "GET", f"/reports/quotations?{rng}", buyer.token)
    check("…the quotation sent to them", out["summary"].get("total") == 1, out["summary"])
    expect("driver: no orders report", "GET", f"/reports/orders?{rng}", driver.token, status=403)
    expect("driver: no quotations report", "GET", f"/reports/quotations?{rng}", driver.token, status=403)
    expect("driver: their trips report", "GET", f"/reports/deliveries?{rng}", driver.token)
    out = expect("rival owner: orders report", "GET", f"/reports/orders?{rng}", rival.token)
    check("…sees none of another nursery's orders", out["summary"].get("total") == 0, out["summary"])
    out = expect("rival cannot widen with nursery_id", "GET", f"/reports/orders?{rng}&nursery_id={nid}", rival.token)
    check("…still none", out["summary"].get("total") == 0, out["summary"])
    out = expect("admin: one nursery", "GET", f"/reports/orders?{rng}&nursery_id={nid}", boss.token)
    check("…all 3", out["summary"].get("total") == 3, out["summary"])
    out = expect("an empty range", "GET", "/reports/orders?from=2020-01-01&to=2020-01-31", owner.token)
    check("…zero and an empty list", out["summary"].get("total") == 0 and out.get("rows") == [], out)

    expect("unknown report type", "GET", f"/reports/payments?{rng}", owner.token, status=400)
    expect("dates backwards", "GET", "/reports/orders?from=2026-02-01&to=2026-01-01", owner.token, status=400)
    expect("more than 3 years", "GET", "/reports/orders?from=2020-01-01&to=2023-01-02", owner.token, status=400)
    expect("dates missing", "GET", "/reports/orders", owner.token, status=400)
    expect("xlsx is not a format", "GET", f"/reports/orders?{rng}&format=xlsx", owner.token, status=400)
    expect("signed out", "GET", f"/reports/orders?{rng}", None, status=401)

    def csv_of(kind):
        req = urllib.request.Request(API + f"/reports/{kind}?{rng}&format=csv")
        req.add_header("Authorization", "Bearer " + owner.token)
        with urllib.request.urlopen(req, timeout=30) as r:
            return r.headers.get("Content-Type", ""), r.headers.get("Content-Disposition", ""), r.read().decode()

    ctype, disp, body = csv_of("orders")
    lines = body.strip().splitlines()
    check("CSV: text/csv attachment named by type and dates", ctype.startswith("text/csv") and f'greenroot-orders-{today}-to-{today}.csv' in disp, [ctype, disp])
    check("…header and 3 rows", lines[0] == "Reference,Date,Customer,Mobile,Status,Amount (INR),Paid (INR)" and len(lines) == 4, lines)
    check("…phone numbers masked, never in full", buyer.mobile not in body and buyer.mobile[-3:] in body, body[:300])
    _, _, body = csv_of("quotations")
    check("…a formula in a customer name is neutralised (quotations CSV)", "'=HYPERLINK" in body and ',=HYPERLINK' not in body, body[:300])


def journey_notifications():
    """Notifications: the unread count ignores paging; read and delete stay with the owner."""
    boss = admin()
    u = User(first="Inbox", activity="CUSTOMER")
    other = User(first="Nosy", activity="CUSTOMER")
    ids = [expect("admin sends a notification", "POST", "/notifications", boss.token,
                  {"user_id": u.id, "title": f"N{i}", "message": f"message {i}"}, status=201)["notification"]["id"]
           for i in range(25)]
    out = expect("first page", "GET", "/notifications?page=1&per_page=20", u.token)
    check("…20 shown, unread_count 25", len(out["notifications"]) == 20 and out.get("unread_count") == 25, out.get("unread_count"))
    for i in ids[5:]:  # read the 20 newest; the 5 oldest stay unread
        call("PUT", f"/notifications/{i}/read", u.token, {})
    out = expect("first page again", "GET", "/notifications?page=1&per_page=20", u.token)
    check("…the newest 20 are read, yet unread_count is 5 (older ones)",
          all(n.get("read_at") for n in out["notifications"]) and out.get("unread_count") == 5, out.get("unread_count"))
    out = expect("unread filter", "GET", "/notifications?unread=true", u.token)
    check("…same count whatever the filter", out.get("unread_count") == 5 and len(out["notifications"]) == 5, out.get("unread_count"))
    check("…another user's count is their own", expect("other's list", "GET", "/notifications", other.token).get("unread_count") == 0)
    expect("another user cannot read it", "GET", f"/notifications/{ids[0]}", other.token, status=(403, 404))
    expect("…nor mark it read", "PUT", f"/notifications/{ids[0]}/read", other.token, {}, status=(403, 404))
    expect("…nor delete it", "DELETE", f"/notifications/{ids[0]}", other.token, status=(403, 404))

    # Clear read: only the caller's read ones; unread and other people's stay
    expect("admin sends the other user one", "POST", "/notifications", boss.token, {"user_id": other.id, "title": "theirs"}, status=201)
    out = expect("the other user clears their read ones", "DELETE", "/notifications/read", other.token)
    check("…none of theirs is read, and none of the inbox user's is touched", out.get("deleted") == 0, out)
    out = expect("clear read", "DELETE", "/notifications/read", u.token)
    check("…the 20 read ones deleted", out.get("deleted") == 20, out)
    out = expect("what is left", "GET", "/notifications?per_page=100", u.token)
    check("…exactly the 5 unread remain", len(out["notifications"]) == 5 and all(not n.get("read_at") for n in out["notifications"]) and out.get("unread_count") == 5,
          [n.get("id") for n in out["notifications"]])
    out = expect("the other user still has theirs", "GET", "/notifications", other.token)
    check("…unread, untouched", out.get("unread_count") == 1, out.get("unread_count"))
    out = expect("clearing again", "DELETE", "/notifications/read", u.token)
    check("…deletes nothing (unread are kept)", out.get("deleted") == 0, out)


JOURNEYS = [
    ("auth", journey_auth),
    ("accounts", journey_accounts),
    ("nursery", journey_nursery),
    ("quotation", journey_quotation),
    ("order", journey_order),
    ("dispatch", journey_dispatch),
    ("top_items", journey_top_items),
    ("plant_suggestions", journey_plant_suggestions),
    ("storage", journey_storage),
    ("payments", journey_payments),
    ("market", journey_market),
    ("wanted", journey_wanted),
    ("moderation", journey_moderation),
    ("admin", journey_admin),
    ("redis", journey_redis),
    ("roles", journey_roles),
    ("access", journey_access),
    ("reports", journey_reports),
    ("notifications", journey_notifications),
]


def main():
    only = {s.strip() for s in args.only.split(",") if s.strip()}
    started = time.time()
    for name, fn in JOURNEYS:
        if only and name not in only:
            continue
        current["journey"] = name
        print(f"\n▶ {name}: {fn.__doc__.strip().splitlines()[0]}")
        try:
            fn()
        except CheckFailed as e:
            print(f"    (journey stopped: {e})")
        except Exception:  # a crash in a journey is a failure, not a stop of the run
            results["fail"] += 1
            results["failures"].append(f"{name}: crashed\n{traceback.format_exc()}")
            print(traceback.format_exc())
    print(f"\n{results['pass']} passed, {results['fail']} failed in {time.time() - started:.1f}s")
    for f in results["failures"]:
        print("  ✗ " + f)
    sys.exit(1 if results["fail"] else 0)


if __name__ == "__main__":
    main()
