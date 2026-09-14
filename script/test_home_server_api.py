import base64
import contextlib
import io
import json
import os
import tempfile
import unittest
from pathlib import Path

import home_server_api as client

ACCESS_KEY = "ak_cq_" + "A" * 22
SECRET_KEY = "sk_cq_" + "S" * 43
ACCESS_TOKEN = "at_cq_" + "T" * 43


class FakeTransport:
    def __init__(self, responses):
        self.responses = list(responses)
        self.calls = []

    def request(self, method, path, *, headers=None, body=None):
        self.calls.append({"method": method, "path": path, "headers": dict(headers or {}), "body": body})
        return self.responses.pop(0)


class FakeRawResponse:
    status = 302

    def getheaders(self):
        return [("Location", "https://evil.invalid/collect")]

    def read(self):
        return b"redirect"


class FakeConnection:
    def __init__(self):
        self.requests = []
        self.closed = False

    def request(self, method, path, body=None, headers=None):
        self.requests.append((method, path, body, dict(headers or {})))

    def getresponse(self):
        return FakeRawResponse()

    def close(self):
        self.closed = True


class ValidationTest(unittest.TestCase):
    def test_base_url_must_be_an_https_origin(self):
        self.assertEqual(client.validate_base_url("https://home.example:18443/"), "https://home.example:18443")
        for value in (
            "http://home.example",
            "https://home.example/api",
            "https://user:pass@home.example",
            "https://home.example?next=/api",
        ):
            with self.subTest(value=value), self.assertRaises(client.ClientError):
                client.validate_base_url(value)

    def test_path_is_origin_relative_and_cannot_select_another_origin(self):
        self.assertEqual(client.validate_request_path("/api/web-projects?limit=20"), "/api/web-projects?limit=20")
        for value in (
            "https://home.example/api/web-projects",
            "//evil.invalid/collect",
            "/api/web-projects#fragment",
            "/api\\evil",
            "/api/\nnext",
        ):
            with self.subTest(value=value), self.assertRaises(client.ClientError):
                client.validate_request_path(value)

    @unittest.skipUnless(os.name == "posix", "Unix permission check")
    def test_credentials_file_must_be_private(self):
        with tempfile.TemporaryDirectory() as directory:
            path = Path(directory) / "credentials.json"
            path.write_text(json.dumps({"access_key": ACCESS_KEY, "secret_key": SECRET_KEY}))
            path.chmod(0o644)

            with self.assertRaisesRegex(client.ClientError, "chmod 600"):
                client.load_credentials(path, {})

            path.chmod(0o600)
            credentials = client.load_credentials(path, {})
            self.assertEqual(credentials.access_key, ACCESS_KEY)
            self.assertEqual(credentials.secret_key, SECRET_KEY)

    def test_credentials_must_match_the_server_generated_formats(self):
        with self.assertRaises(client.ClientError):
            client.load_credentials(None, {
                client.ACCESS_KEY_ENV: "ak_cq_short",
                client.SECRET_KEY_ENV: "sk_cq_short",
            })


class RequestTest(unittest.TestCase):
    # 使用 FakeTransport 核对先以 Basic 换 Token、再以 Bearer 调业务路径的顺序和请求内容，不发网络请求。
    def test_client_exchanges_basic_credentials_then_calls_same_origin_with_bearer(self):
        transport = FakeTransport([
            client.Response(200, {"content-type": "application/json"}, json.dumps({
                "access_token": ACCESS_TOKEN,
                "token_type": "Bearer",
                "expires_in": 900,
                "scope": "web-projects:read",
            }).encode()),
            client.Response(200, {"content-type": "application/json"}, json.dumps({
                "code": 0,
                "message": "success",
                "data": {"items": [{"id": "12"}]},
            }).encode()),
        ])
        api = client.HomeServerAPI(transport, client.Credentials(ACCESS_KEY, SECRET_KEY))

        response = api.call("GET", "/api/web-projects")

        self.assertEqual(response.status, 200)
        self.assertEqual(transport.calls[0]["path"], "/api/auth/token")
        self.assertEqual(transport.calls[0]["body"], b"grant_type=client_credentials")
        scheme, encoded = transport.calls[0]["headers"]["Authorization"].split(" ", 1)
        self.assertEqual(scheme, "Basic")
        self.assertEqual(base64.b64decode(encoded), f"{ACCESS_KEY}:{SECRET_KEY}".encode())
        self.assertEqual(transport.calls[1]["headers"]["Authorization"], f"Bearer {ACCESS_TOKEN}")

    def test_http_transport_returns_redirect_without_following_or_forwarding_headers(self):
        connection = FakeConnection()
        transport = client.HTTPTransport(
            "https://home.example",
            timeout=3,
            connection_factory=lambda: connection,
        )

        response = transport.request(
            "GET",
            "/api/web-projects",
            headers={"Authorization": f"Bearer {ACCESS_TOKEN}"},
        )

        self.assertEqual(response.status, 302)
        self.assertEqual(response.headers["location"], "https://evil.invalid/collect")
        self.assertEqual(len(connection.requests), 1)
        self.assertEqual(connection.requests[0][1], "/api/web-projects")
        self.assertTrue(connection.closed)

    # 构造会回显认证材料的失败响应，验证最终 ClientError 不泄露 AK/SK、Token 或 Basic/Bearer 片段。
    def test_failures_do_not_include_credentials_or_access_tokens(self):
        access_key = "ak_cq_do-not-print"
        secret_key = "sk_cq_do-not-print"
        access_token = "at_cq_do-not-print"
        echoed = f"bad {access_key} {secret_key} Basic Zm9v Bearer {access_token}"
        transport = FakeTransport([
            client.Response(401, {"content-type": "application/json"}, json.dumps({
                "error": "invalid_client",
                "error_description": echoed,
            }).encode()),
        ])
        api = client.HomeServerAPI(transport, client.Credentials(access_key, secret_key))

        with self.assertRaises(client.ClientError) as raised:
            api.call("GET", "/api/web-projects")

        message = str(raised.exception)
        self.assertNotIn(access_key, message)
        self.assertNotIn(secret_key, message)
        self.assertNotIn(access_token, message)
        self.assertNotIn("Basic Zm9v", message)
        self.assertNotIn("Bearer", message)

    def test_token_failure_does_not_include_base64_encoded_basic_credentials(self):
        encoded_credentials = base64.b64encode(f"{ACCESS_KEY}:{SECRET_KEY}".encode()).decode()
        transport = FakeTransport([
            client.Response(401, {"content-type": "application/json"}, json.dumps({
                "error": "invalid_client",
                "error_description": f"rejected credentials {encoded_credentials}",
            }).encode()),
        ])
        api = client.HomeServerAPI(transport, client.Credentials(ACCESS_KEY, SECRET_KEY))

        with self.assertRaises(client.ClientError) as raised:
            api.exchange_token()

        self.assertNotIn(encoded_credentials, str(raised.exception))

    def test_business_output_redacts_authentication_fields_and_values(self):
        result = {
            "id": "12",
            "access_key": "ak_cq_do-not-print",
            "nested": {
                "secret_key": "sk_cq_do-not-print",
                "note": "issued Bearer at_cq_do-not-print",
            },
        }

        rendered = json.dumps(client.redact_output(
            result,
            ("ak_cq_do-not-print", "sk_cq_do-not-print", "at_cq_do-not-print"),
        ))

        self.assertNotIn("ak_cq_do-not-print", rendered)
        self.assertNotIn("sk_cq_do-not-print", rendered)
        self.assertNotIn("at_cq_do-not-print", rendered)
        self.assertIn('"id": "12"', rendered)

    # 用临时 ZIP 和 FakeTransport 验证 multipart 字段、If-Match 及文件字节，并确认只换一次 Token、发送一次业务请求。
    def test_multipart_upload_and_if_match_are_sent_once(self):
        with tempfile.TemporaryDirectory() as directory:
            upload = Path(directory) / "site.zip"
            upload.write_bytes(b"PK\x03\x04fixture")
            body, content_type = client.build_multipart(upload, "public/index.html", boundary="fixture-boundary")
            transport = FakeTransport([
                client.Response(200, {"content-type": "application/json"}, json.dumps({
                    "access_token": ACCESS_TOKEN,
                    "token_type": "Bearer",
                    "expires_in": 900,
                    "scope": "web-projects:write",
                }).encode()),
                client.Response(201, {"content-type": "application/json"}, b'{"code":0,"message":"success","data":{"id":"9"}}'),
            ])
            api = client.HomeServerAPI(transport, client.Credentials(ACCESS_KEY, SECRET_KEY))

            api.call("POST", "/api/web-projects/12/releases", body=body, headers={
                "Content-Type": content_type,
                "If-Match": "7",
            })

        request = transport.calls[1]
        self.assertEqual(request["headers"]["If-Match"], "7")
        self.assertEqual(request["headers"]["Content-Type"], "multipart/form-data; boundary=fixture-boundary")
        self.assertIn(b'name="entry_file"\r\n\r\npublic/index.html', request["body"])
        self.assertIn(b'name="file"; filename="site.zip"', request["body"])
        self.assertIn(b"PK\x03\x04fixture", request["body"])
        self.assertEqual(len(transport.calls), 2)

    @unittest.skipUnless(hasattr(os, "symlink"), "Symbolic links are not supported")
    def test_multipart_upload_rejects_a_symbolic_link_before_reading_it(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            private_target = root / "secret.txt"
            private_target.write_text("private-target-content", encoding="utf-8")
            upload = root / "site.html"
            upload.symlink_to(private_target)

            with self.assertRaises(client.ClientError):
                client.build_multipart(upload, None, boundary="fixture-boundary")


class ArgumentPrivacyTests(unittest.TestCase):
    def test_unsupported_secret_argument_is_not_echoed(self):
        output = io.StringIO()
        with contextlib.redirect_stderr(output), self.assertRaises(SystemExit):
            client.parse_args(["--path", "/api/web-projects", "--secret-key", SECRET_KEY])
        self.assertNotIn(SECRET_KEY, output.getvalue())


    def test_credential_shaped_file_error_is_not_echoed(self):
        output = io.StringIO()
        with contextlib.redirect_stderr(output):
            code = client.main(["--base-url", "https://home.example", "--path", "/api/web-projects", "--credentials-file", SECRET_KEY])
        self.assertEqual(code, 1)
        self.assertNotIn(SECRET_KEY, output.getvalue())


if __name__ == "__main__":
    unittest.main()
