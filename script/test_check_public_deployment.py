#!/usr/bin/env python3
import unittest

from script.check_public_deployment import has_forbidden_deployment_url


class DeploymentURLTest(unittest.TestCase):
    def test_allows_exact_nginx_host_redirect(self):
        self.assertFalse(has_forbidden_deployment_url("    return 302 https://$host:8080$request_uri;"))
        self.assertFalse(has_forbidden_deployment_url("\treturn 307 https://$host:443$request_uri;\t"))

    def test_rejects_real_domains_and_non_exact_variables(self):
        invalid_lines = (
            "return 302 https://home.company.test:8080$request_uri;",
            "return 302 https://$server_name:8080$request_uri;",
            "return 301 https://$host:8080$request_uri;",
            "return 302 https://$host:70000$request_uri;",
        )
        for line in invalid_lines:
            with self.subTest(line=line):
                self.assertTrue(has_forbidden_deployment_url(line))


if __name__ == "__main__":
    unittest.main()
