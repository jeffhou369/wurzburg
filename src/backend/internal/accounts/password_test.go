package accounts

import (
	"strings"
	"testing"
)

func TestValidatePasswordPolicyCountsUnicodeCharacters(t *testing.T) {
	if err := ValidatePasswordPolicy("密碼測試", 12); err == nil {
		t.Fatal("少於最低 Unicode 字元數的密碼應被拒絕")
	}
	if err := ValidatePasswordPolicy(strings.Repeat("密", 12), 12); err != nil {
		t.Fatalf("達到最低 Unicode 字元數的密碼應通過：%v", err)
	}
}
