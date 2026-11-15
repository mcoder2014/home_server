package accounts

import (
	"context"
	"testing"

	"golang.org/x/crypto/bcrypt"
)

// BenchmarkVerifyPasswordWebDAV compares production-cost password checks with
// repeated DAV credentials, excluding hash generation and the initial warm-up.
func BenchmarkVerifyPasswordWebDAV(b *testing.B) {
	password := "SyntheticWebDAVBenchmarkPassword123"
	hash, err := bcrypt.GenerateFromPassword([]byte(password), 12)
	if err != nil {
		b.Fatal(err)
	}
	for _, scope := range []string{"login_bcrypt", PasswordBudgetAuthenticated, PasswordBudgetWebDAV} {
		b.Run(scope, func(b *testing.B) {
			ctx := context.WithValue(context.Background(), PasswordSourceIPKey, "198.51.100.181")
			dav := context.WithValue(ctx, PasswordBudgetScopeKey, PasswordBudgetWebDAV)
			if err := VerifyPassword(dav, 9181, "benchmark", string(hash), password); err != nil {
				b.Fatal(err)
			}
			if scope != "login_bcrypt" {
				ctx = context.WithValue(ctx, PasswordBudgetScopeKey, scope)
			}
			if err := VerifyPassword(ctx, 9181, "benchmark", string(hash), password); err != nil {
				b.Fatal(err)
			}
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				if err := VerifyPassword(ctx, 9181, "benchmark", string(hash), password); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}
