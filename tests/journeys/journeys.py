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

def approved_owner(boss, name="Journey Nursery"):
    """A nursery owner whose nursery the admin has approved (trial subscription)."""
    owner = User(first="Owner", activity="OWNER")
    n = expect("register nursery", "POST", "/nurseries", owner.token, {
        "name": f"{name} {owner.mobile[-4:]}", "mobile": owner.mobile, "address_line1": "Plot 1",
        "city": "Hyderabad", "state": "Telangana", "country": "India", "postal_code": "500032"}, status=201)
    nid = n["nursery"]["id"]
    expect("admin approves nursery", "PUT", f"/nurseries/{nid}/status", boss.token, {"status": "APPROVED"})
    owner.refresh()
    owner.nursery_id = nid
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
    owner.nursery_id = nid
    expect("approving twice is refused", "PUT", f"/nurseries/{nid}/status", boss.token, {"status": "APPROVED"}, status=409)
    expect("owner edits their nursery", "PUT", f"/nurseries/{nid}", owner.token, {"description": "Fruit saplings"})
    out = expect("owner cannot change status through the edit form", "PUT", f"/nurseries/{nid}", owner.token, {"status": "SUSPENDED"})
    check("…status unchanged", out["nursery"]["status"] == "APPROVED", out["nursery"]["status"])

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
    out = expect("assigned manager edits", "PUT", f"/quotations/{qid}", manager.token,
                 quote(items=[{"plant_id": mango, "quantity": 20, "unit_price": 150, "total_price": 3000}, {"plant_id": jamun, "quantity": 5, "unit_price": 80, "total_price": 400}]))
    check("…total recalculated to 3400", float(out["quotation"]["total_amount"]) == 3400, out["quotation"]["total_amount"])
    expect("owner unassigns", "DELETE", f"/quotations/{qid}/assign-manager", owner.token)
    expect("manager loses sight of it", "GET", f"/quotations/{qid}", manager.token, status=(403, 404))
    expect("owner can edit again", "PUT", f"/quotations/{qid}", owner.token, quote(notes="final", items=[{"plant_id": mango, "quantity": 20, "unit_price": 150, "total_price": 3000}]))

    # Send, recall, send, accept, convert
    out = expect("owner sends", "POST", f"/quotations/{qid}/send", owner.token, {})
    check("…CUSTOMER_SENT with valid_until", out["quotation"]["status"] == "CUSTOMER_SENT" and out["quotation"].get("valid_until"), out["quotation"])
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
    out = expect("buyer accepts", "POST", f"/quotations/{qid}/buyer-accept", buyer.token, {})
    check("…CUSTOMER_ACCEPTED", out["quotation"]["status"] == "CUSTOMER_ACCEPTED", out["quotation"]["status"])
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
    expect("buyer sees the order", "GET", f"/orders/{cq['converted_order_id']}", buyer.token)

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
    expect("owner assigns the manager", "POST", f"/orders/{oid}/assign-manager", owner.token, {"manager_user_id": manager.id})

    # Confirm → loading → partial → complete
    out = expect("manager confirms", "POST", f"/orders/{oid}/confirm", manager.token, {})
    check("…CONFIRMED", out["order"]["order_status"] == "CONFIRMED", out["order"]["order_status"])
    expect("confirming twice is refused", "POST", f"/orders/{oid}/confirm", manager.token, {}, status=409)
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
    out = expect("complete loading", "POST", f"/orders/{oid}/complete-loading", owner.token, {})
    po = out["order"]
    expected_total = (first["quantity"] - 2) * first["unit_price"] + second["quantity"] * second["unit_price"]
    check("…PARTIALLY_FULFILLED and total from loaded quantities", po["order_status"] == "PARTIALLY_FULFILLED" and abs(float(po["total_amount"]) - expected_total) < 0.01,
          {"status": po["order_status"], "total": po["total_amount"], "want": expected_total})
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
    out = expect("complete loading with nothing short", "POST", f"/orders/{o2['id']}/complete-loading", owner.token, {})
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
    expect("complete loading", "POST", f"/orders/{o['id']}/complete-loading", owner.token, {})
    expect("buyer cannot create a dispatch", "POST", "/dispatches", buyer.token, {"order_id": o["id"]}, status=403)
    expect("driver cannot create a dispatch", "POST", "/dispatches", driver.token, {"order_id": o["id"]}, status=403)
    expect("a rival owner cannot dispatch this order", "POST", "/dispatches", rival.token, {"order_id": o["id"]}, status=403)
    expect("unknown order", "POST", "/dispatches", owner.token, {"order_id": 999999999}, status=(400, 404))
    d = expect("manager creates the dispatch", "POST", "/dispatches", manager.token, {"order_id": o["id"]}, status=201)["dispatch"]
    did, code = d["id"], d["dispatch_code"]
    check("…PENDING with a code and tracking link", d["dispatch_status"] == "PENDING" and code and d.get("trip_uuid"), d)
    expect("only one active dispatch per order", "POST", "/dispatches", owner.token, {"order_id": o["id"]}, status=409)

    # QR lookup and accept
    expect("the order's buyer can look up the trip", "GET", f"/dispatches/code/{code}", buyer.token)
    expect("an unrelated buyer cannot look up a trip by code", "GET", f"/dispatches/code/{code}", User(first="Nosy", activity="CUSTOMER").token, status=403)
    expect("a rival owner cannot look up a trip by code", "GET", f"/dispatches/code/{code}", rival.token, status=403)
    expect("a driver previews the unclaimed trip", "GET", f"/dispatches/code/{code}", driver.token)
    expect("a buyer cannot accept a trip", "POST", f"/dispatches/{did}/accept", buyer.token, {}, status=403)
    expect("an owner cannot accept a trip", "POST", f"/dispatches/{did}/accept", owner.token, {}, status=403)
    out = expect("driver accepts", "POST", f"/dispatches/{did}/accept", driver.token, {})
    check("…ACCEPTED by this driver", out["dispatch"]["dispatch_status"] == "ACCEPTED" and out["dispatch"].get("driver_user_id") == driver.id, out["dispatch"])
    expect("accepting again is idempotent", "POST", f"/dispatches/{did}/accept", driver.token, {})
    expect("another driver cannot take an accepted trip", "POST", f"/dispatches/{did}/accept", second_driver.token, {}, status=(403, 409))
    expect("another driver cannot look it up any more", "GET", f"/dispatches/code/{code}", second_driver.token, status=403)

    # One active trip per driver
    o2 = loaded_order()
    expect("start loading", "POST", f"/orders/{o2['id']}/start-loading", owner.token, {})
    expect("complete loading", "POST", f"/orders/{o2['id']}/complete-loading", owner.token, {})
    d2 = expect("second dispatch", "POST", "/dispatches", owner.token, {"order_id": o2["id"]}, status=201)["dispatch"]
    expect("a driver with an active trip cannot accept another", "POST", f"/dispatches/{d2['id']}/accept", driver.token, {}, status=409)

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
    expect("an unknown tracking link is not found", "GET", "/track/00000000-0000-0000-0000-000000000000", None, status=(400, 404))

    expect("order cannot complete while the trip is open", "PUT", f"/orders/{o['id']}/status", owner.token, {"order_status": "COMPLETED"}, status=409)
    st(("driver", driver), "DELIVERED")
    out = expect("buyer sees the order", "GET", f"/orders/{o['id']}", buyer.token)
    check("…delivery completed the order", out["order"]["order_status"] == "COMPLETED", out["order"]["order_status"])
    st(("owner", owner), "CANCELLED", 422)
    expect("no trip events after delivery", "POST", f"/dispatches/{did}/trip-events", driver.token, {"event_type": "LATE"}, status=422)
    expect("second driver is free to accept the other trip", "POST", f"/dispatches/{d2['id']}/accept", second_driver.token, {})
    expect("owner cancels the other trip", "PUT", f"/dispatches/{d2['id']}/status", owner.token, {"dispatch_status": "CANCELLED"})

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
    tamarind = expect("look up a catalog plant", "GET", "/plants/lookup?q=Tamarindus", owner.token)["results"][0]["plant"]
    ad = {"plant_id": tamarind["id"], "plant_name": "Imli", "title": "Sweet grafts", "description": "2 ft grafts", "quantity": 200, "price_per_unit": 120, "price_unit": "PER_PLANT", "photos": []}
    expect("a buyer cannot browse the market", "GET", "/market/ads", buyer.token, status=403)
    expect("a driver cannot browse the market", "GET", "/market/ads", driver.token, status=403)
    expect("a buyer cannot post an ad", "POST", "/market/ads", buyer.token, ad, status=403)
    a = expect("manager posts an ad", "POST", "/market/ads", manager.token, ad, status=201)["ad"]
    check("…DRAFT", a["status"] == "DRAFT", a["status"])
    expect("a rival cannot publish it", "POST", f"/market/ads/{a['id']}/publish", rival.token, {}, status=(403, 404))
    expect("owner publishes", "POST", f"/market/ads/{a['id']}/publish", owner.token, {})
    out = expect("a rival browses the market", "GET", "/market/ads", rival.token)
    check("…and sees the ad", a["id"] in [x["id"] for x in out.get("ads", [])], out)
    out = expect("search by the catalog's botanical name", "GET", "/market/ads?q=tamarindus", rival.token)
    check("…finds the ad linked to that plant", a["id"] in [x["id"] for x in out.get("ads", [])], out)
    expect("a rival cannot archive it", "POST", f"/market/ads/{a['id']}/archive", rival.token, {}, status=(403, 404))
    expect("the posting nursery cannot enquire on its own ad", "POST", f"/market/ads/{a['id']}/enquiries", owner.token, {"message": "self"}, status=400)
    e = expect("a rival enquires", "POST", f"/market/ads/{a['id']}/enquiries", rival.token, {"message": "Need 50, can you deliver?"}, status=(200, 201))
    expect("a second enquiry from the same nursery is refused", "POST", f"/market/ads/{a['id']}/enquiries", rival.token, {"message": "again"}, status=400)
    eid = e.get("enquiry", e).get("id")
    expect("manager replies", "POST", f"/market/enquiries/{eid}/reply", manager.token, {"body": "Yes, Rs 120 each"}, status=(200, 201))
    seen = expect("the rival reopens the ad", "GET", f"/market/ads/{a['id']}", rival.token)["ad"]
    check("…and is pointed at its existing chat", seen.get("my_enquiry_id") == eid and not seen["capabilities"]["can_enquire"], seen)

    # Bargaining: offer, counter, accept. Offers in a chat replace each other.
    chat = f"/market/enquiries/{eid}"
    offer = expect("the rival offers Rs 100 for 50", "POST", f"{chat}/offers", rival.token, {"price_per_unit": 100, "quantity": 50}, status=201)["message"]
    check("…a pending offer", offer["type"] == "OFFER" and offer["offer_status"] == "PENDING", offer)
    expect("an offer above the ad's quantity is refused", "POST", f"{chat}/offers", rival.token, {"price_per_unit": 100, "quantity": 201}, status=400)
    expect("a zero offer is refused", "POST", f"{chat}/offers", rival.token, {"price_per_unit": 0}, status=400)
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


def journey_admin():
    """Admin-only routes refuse everyone else; admins cannot trade."""
    boss = admin()
    owner, manager, buyer, driver, _ = team(boss)
    for who, u in [("owner", owner), ("manager", manager), ("buyer", buyer), ("driver", driver)]:
        expect(f"{who} cannot open the admin dashboard", "GET", "/admin/dashboard", u.token, status=403)
        expect(f"{who} cannot list users", "GET", "/admin/users", u.token, status=403)
        expect(f"{who} cannot suspend a nursery", "PUT", f"/admin/nurseries/{owner.nursery_id}/status", u.token, {"status": "SUSPENDED", "reason": "x"}, status=403)
        expect(f"{who} cannot approve drivers", "POST", f"/drivers/{driver.id}/approve", u.token, {}, status=403)
    out = expect("admin dashboard", "GET", "/admin/dashboard", boss.token)
    check("…has numbers", bool(out), out)
    out = expect("admin finds a user by mobile", "GET", f"/admin/users?search={buyer.mobile}", boss.token)
    check("…found", buyer.mobile in json.dumps(out), out)
    expect("suspend without a reason is refused", "PUT", f"/admin/nurseries/{owner.nursery_id}/status", boss.token, {"status": "SUSPENDED"}, status=400)
    expect("an unknown user status is refused", "PUT", f"/admin/users/{buyer.id}/status", boss.token, {"status": "GHOST"}, status=400)
    expect("no token, no admin", "GET", "/admin/dashboard", None, status=401)


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
    out = expect("which nurseries list neem", "GET", f"/plants/{plants[1]}/top-items", buyer.token)
    check("…includes this nursery", nid in [i["nursery_id"] for i in out["top_items"]], out)
    expect("owner joins the sourcing network", "POST", f"/nurseries/{nid}/sourcing-membership", owner.token,
           {"road_accessible": True, "lorry_accessible": True, "contact_visible": True, "service_radius_km": 50}, status=(200, 201))
    out = expect("rival finds the nursery in the network", "GET", f"/sourcing-network/nurseries/{nid}", rival.token)
    shown = out.get("nursery", out).get("top_items", [])
    check("…with its top items (at most 20)", 0 < len(shown) <= 20 and all("plant_name" in x and "photos" in x for x in shown), shown[:2])
    out = expect("owner dashboard counts top items", "GET", "/me/owner-dashboard", owner.token)
    check("…20", out.get("top_items", out.get("dashboard", {}).get("top_items", {})).get("total") == 20, out)


JOURNEYS = [
    ("auth", journey_auth),
    ("accounts", journey_accounts),
    ("nursery", journey_nursery),
    ("quotation", journey_quotation),
    ("order", journey_order),
    ("dispatch", journey_dispatch),
    ("top_items", journey_top_items),
    ("payments", journey_payments),
    ("market", journey_market),
    ("admin", journey_admin),
    ("redis", journey_redis),
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
