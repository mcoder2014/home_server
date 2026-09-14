#!/usr/bin/env python3
"""Reject environment-specific data in public deployment/config examples."""
import ipaddress
from pathlib import Path
import re
import sys
from urllib.parse import urlsplit

ROOT = Path(__file__).resolve().parents[1]
DOCUMENT_NETS = tuple(ipaddress.ip_network(cidr) for cidr in ("192.0.2.0/24", "198.51.100.0/24", "203.0.113.0/24", "2001:db8::/32"))
EXAMPLE_DOMAIN = re.compile(r"(?:[a-zA-Z0-9*-]+\.)*example\.(?:com|net|org)")


def is_example_address(value):
    try:
        address = ipaddress.ip_address(value)
    except ValueError:
        return False
    return address.is_loopback or address.is_unspecified or any(address in network for network in DOCUMENT_NETS)


# 逐行检查公开部署示例中的地址、个人目录、凭证和服务账号，返回带文件位置的违规项；只读取文件。
def check_file(path):
    errors = []
    for number, line in enumerate(path.read_text().splitlines(), 1):
        reason = None
        address_literals = re.findall(r"(?<![\w.])(?:\d{1,3}\.){3}\d{1,3}(?![\w.])|(?<![\w:])[0-9A-Fa-f:]{3,}(?![\w:])", line)
        for value in address_literals:
            try:
                address = ipaddress.ip_address(value)
            except ValueError:
                continue
            if not (address.is_loopback or address.is_unspecified or any(address in network for network in DOCUMENT_NETS)):
                reason = "IP literals must be loopback, bind-all, or documentation addresses"
        if re.search(r"/(?:Users|home)/[A-Za-z0-9_.-]+", line):
            reason = "personal home directory in public example"
        if re.search(r"-----BEGIN (?:RSA |EC |OPENSSH )?PRIVATE KEY-----", line):
            reason = "private key in public example"
        credential = re.match(r"\s*(?:app_code|api_key|api_token|secret_key)\s*:\s*(.*?)\s*(?:#.*)?$", line)
        if credential:
            value = credential.group(1).strip("\"'")
            if value and value not in ("xxxx", "REPLACE_ME") and not value.startswith(("<", "${")):
                reason = "credential must be empty or a placeholder"
        account = re.match(r"\s*(?:User|Group)=(\S+)", line)
        if account and account.group(1) not in ("home-server", "www-data", "root", "nobody"):
            reason = "service account must be a generic example"
        domain = re.match(r"\s*server_name\s+([^;]+);", line)
        if domain and any(name != "_" and name != "localhost" and not is_example_address(name) and not EXAMPLE_DOMAIN.fullmatch(name) for name in domain.group(1).split()):
            reason = "server names must use reserved example domains"
        fixture_domain = re.search(r'(?:Domain:\s*|GetAllDNSRecord\([^\n]*,\s*)"([^"/]+\.[^"/]+)"', line)
        if fixture_domain and not EXAMPLE_DOMAIN.fullmatch(fixture_domain.group(1)):
            reason = "DNS test fixtures must use reserved example domains"
        if path.is_relative_to(ROOT / "deploy") or path.is_relative_to(ROOT / "config/systemd"):
            for url in re.findall(r"https?://[^\s\"'`<>]+", line):
                parsed = urlsplit(url.rstrip(").,;]"))
                if parsed.username or parsed.password or (parsed.hostname and parsed.hostname != "localhost" and not is_example_address(parsed.hostname) and not EXAMPLE_DOMAIN.fullmatch(parsed.hostname)):
                    reason = "deployment example URLs must use reserved domains without credentials"
        if reason:
            errors.append(f"{path.relative_to(ROOT)}:{number}: {reason}")
    return errors


def main():
    paths = list((ROOT / "deploy/pi").glob("*"))
    paths += [ROOT / "config/config_example.yaml", ROOT / "front_vue/src/global.js", ROOT / "front_vue/src/components/Common.vue", ROOT / "README.md", ROOT / "rpc/cloudflare_test.go"]
    paths += list((ROOT / "config/nginx").glob("*.conf"))
    paths += list((ROOT / "config/systemd").glob("*.service"))
    errors = [error for path in paths if path.is_file() for error in check_file(path)]
    if errors:
        print("\n".join(errors), file=sys.stderr)
        return 1
    print("Public deployment examples passed the scoped environment-data checks.")
    return 0


if __name__ == "__main__":
    sys.exit(main())
