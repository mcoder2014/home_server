package utils

import (
	"testing"

	"golang.org/x/crypto/bcrypt"
)

func TestHashAndSaltUsesProductionPasswordCost(t *testing.T) {
	hash, err := HashAndSalt("Synthetic password for test 42!")
	if err != nil {
		t.Fatal(err)
	}
	cost, err := bcrypt.Cost([]byte(hash))
	if err != nil {
		t.Fatal(err)
	}
	if cost < 10 {
		t.Fatalf("password cost %d is below the supported minimum 10", cost)
	}
	if !ComparePasswords(hash, "Synthetic password for test 42!") || ComparePasswords(hash, "different") {
		t.Fatal("password comparison changed")
	}
}
