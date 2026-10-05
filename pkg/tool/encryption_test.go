package tool

import (
	"crypto/sha512"
	"encoding/hex"
	"strings"
	"testing"

	"golang.org/x/crypto/bcrypt"
	"golang.org/x/crypto/pbkdf2"
)

// legacyPBKDF2Hash builds a hash in the pre-argon2id PPanel format so we can
// prove that already-migrated users keep working after the algorithm switch.
func legacyPBKDF2Hash(password, salt string) string {
	derived := pbkdf2.Key([]byte(password), []byte(salt), legacyPBKDF2Options.Iterations, legacyPBKDF2Options.KeyLen, sha512.New)
	return legacyPBKDF2Prefix + salt + "$" + hex.EncodeToString(derived)
}

func TestEncodePassWord_ProducesArgon2idPHC(t *testing.T) {
	hash := EncodePassWord("password")
	if !strings.HasPrefix(hash, argon2idPrefix) {
		t.Fatalf("expected argon2id PHC prefix, got %q", hash)
	}
	parsed, err := parseArgon2idPHC(hash)
	if err != nil {
		t.Fatalf("parseArgon2idPHC(%q): %v", hash, err)
	}
	if parsed.Params.Memory != defaultArgon2id.Memory ||
		parsed.Params.Iterations != defaultArgon2id.Iterations ||
		parsed.Params.Parallelism != defaultArgon2id.Parallelism {
		t.Fatalf("unexpected params: %+v", parsed.Params)
	}
	if uint32(len(parsed.Salt)) != defaultArgon2id.SaltLen {
		t.Fatalf("salt length = %d, want %d", len(parsed.Salt), defaultArgon2id.SaltLen)
	}
	if uint32(len(parsed.Key)) != defaultArgon2id.KeyLen {
		t.Fatalf("key length = %d, want %d", len(parsed.Key), defaultArgon2id.KeyLen)
	}
}

func TestEncodePassWord_UniqueSaltPerCall(t *testing.T) {
	first := EncodePassWord("password")
	second := EncodePassWord("password")
	if first == second {
		t.Fatal("encoding the same password twice must produce different hashes (random salt)")
	}
}

func TestVerifyPassWord_Argon2idRoundTrip(t *testing.T) {
	hash := EncodePassWord("s3cr3t-p@ss")
	if !VerifyPassWord("s3cr3t-p@ss", hash) {
		t.Fatal("correct password should verify against argon2id hash")
	}
	if VerifyPassWord("wrong", hash) {
		t.Fatal("wrong password must not verify against argon2id hash")
	}
}

func TestVerifyPassWord_LegacyPBKDF2(t *testing.T) {
	hash := legacyPBKDF2Hash("legacy-password", "0123456789abcdef")
	if !VerifyPassWord("legacy-password", hash) {
		t.Fatal("correct password should verify against legacy pbkdf2 hash")
	}
	if VerifyPassWord("nope", hash) {
		t.Fatal("wrong password must not verify against legacy pbkdf2 hash")
	}
}

func TestVerifyPassWord_MalformedLegacyHash(t *testing.T) {
	cases := []string{
		"$pbkdf2-sha512$",
		"$pbkdf2-sha512$onlysalt",
		"$pbkdf2-sha512$salt$",
		"$pbkdf2-sha512$$abc",
		"$pbkdf2-sha256$salt$abc",
		"not-a-hash",
	}
	for _, c := range cases {
		if VerifyPassWord("password", c) {
			t.Fatalf("malformed hash %q must not verify", c)
		}
	}
}

func TestVerifyPassWord_MalformedArgon2id(t *testing.T) {
	cases := []string{
		"$argon2id$",
		"$argon2id$v=19$",
		"$argon2id$v=13$m=19456,t=2,p=1$c2FsdA$a2V5",         // unsupported version
		"$argon2id$v=19$m=19456,t=2,p=1$c2FsdA",              // missing key
		"$argon2id$v=19$m=19456,t=2,p=1$$a2V5",               // missing salt
		"$argon2id$v=19$m=0,t=2,p=1$c2FsdA$a2V5",             // zero memory
		"$argon2id$v=19$m=19456,t=0,p=1$c2FsdA$a2V5",         // zero iterations
		"$argon2id$v=19$m=19456,t=2,p=0$c2FsdA$a2V5",         // zero parallelism
		"$argon2id$v=19$m=19456,t=2,p=1,x=9$c2FsdA$a2V5",     // unknown param
		"$argon2id$v=19$m=19456,t=2$c2FsdA$a2V5",             // missing param
		"$argon2id$v=19$m=19456,m=19456,t=2,p=1$c2FsdA$a2V5", // duplicate param
		"$argon2id$v=19$m=notanumber,t=2,p=1$c2FsdA$a2V5",    // non-numeric param
	}
	for _, c := range cases {
		if VerifyPassWord("password", c) {
			t.Fatalf("malformed argon2id hash %q must not verify", c)
		}
	}
}

func TestVerifyPassWord_Argon2idRejectsOutOfRangeParams(t *testing.T) {
	// A hash claiming an absurd memory cost must be rejected before allocating,
	// otherwise a malicious database row could DoS the login path.
	huge := "$argon2id$v=19$m=4294967295,t=2,p=1$c2FsdHNhbHRzYWx0$a2V5a2V5a2V5a2V5"
	if VerifyPassWord("password", huge) {
		t.Fatal("hash with out-of-range memory must not verify")
	}
	manyIters := "$argon2id$v=19$m=19456,t=1000,p=1$c2FsdHNhbHRzYWx0$a2V5a2V5a2V5a2V5"
	if VerifyPassWord("password", manyIters) {
		t.Fatal("hash with out-of-range iterations must not verify")
	}
}

func TestMultiPasswordVerify_Argon2id(t *testing.T) {
	hash := EncodePassWord("argon-pass")
	if !MultiPasswordVerify(PasswordAlgoArgon2id, "", "argon-pass", hash) {
		t.Fatal("argon2id algo should verify argon2id hash")
	}
	// MultiPasswordVerify must detect the argon2id prefix even if algo is stale,
	// which is exactly what happens when a user was migrated but the column was
	// not updated yet.
	if !MultiPasswordVerify("default", "", "argon-pass", hash) {
		t.Fatal("stale algo should still verify argon2id hash by prefix")
	}
	if MultiPasswordVerify(PasswordAlgoArgon2id, "", "wrong", hash) {
		t.Fatal("wrong password must not verify")
	}
}

func TestMultiPasswordVerify_LegacyAlgos(t *testing.T) {
	// md5("123456") = e10adc3949ba59abbe56e057f20f883e
	if !MultiPasswordVerify("md5", "", "123456", "e10adc3949ba59abbe56e057f20f883e") {
		t.Fatal("md5 verify failed")
	}
	// sha256("123456") = 8d969eef6ecad3c29a3a629280e686cf0c3f5d5a86aff3ca12020c923adc6c92
	if !MultiPasswordVerify("sha256", "", "123456", "8d969eef6ecad3c29a3a629280e686cf0c3f5d5a86aff3ca12020c923adc6c92") {
		t.Fatal("sha256 verify failed")
	}
	// sha256salt("123456" + "ppanel")
	hash := "4fb4d5ec8ec384d63cfe1faf2d9610140b310f68fd72eb0df90d3027b702b35f"
	if !MultiPasswordVerify("sha256salt", "ppanel", "123456", hash) {
		t.Fatal("sha256salt: correct password should verify")
	}
	if MultiPasswordVerify("sha256salt", "ppanel", "wrong", hash) {
		t.Fatal("sha256salt: wrong password must not verify")
	}
}

func TestMultiPasswordVerify_LegacyPBKDF2AsDefault(t *testing.T) {
	hash := legacyPBKDF2Hash("pbkdf2-pass", "abcdef0123456789")
	if !MultiPasswordVerify("default", "", "pbkdf2-pass", hash) {
		t.Fatal("default algo should verify legacy pbkdf2 hash")
	}
	if MultiPasswordVerify("default", "", "wrong", hash) {
		t.Fatal("default algo must reject wrong password")
	}
}

func TestMultiPasswordVerify_Bcrypt(t *testing.T) {
	hash, err := bcrypt.GenerateFromPassword([]byte("admin1"), bcrypt.DefaultCost)
	if err != nil {
		t.Fatalf("bcrypt generate: %v", err)
	}
	if !MultiPasswordVerify("bcrypt", "", "admin1", string(hash)) {
		t.Fatal("bcrypt correct password should verify")
	}
	if MultiPasswordVerify("bcrypt", "", "wrong", string(hash)) {
		t.Fatal("bcrypt wrong password must not verify")
	}
}

func TestMultiPasswordVerify_UnknownAlgo(t *testing.T) {
	if MultiPasswordVerify("rot13", "", "password", "anything") {
		t.Fatal("unknown algo must never verify")
	}
}

func TestPasswordNeedsRehash(t *testing.T) {
	// Freshly encoded passwords never need a rehash.
	fresh := EncodePassWord("password")
	if PasswordNeedsRehash(PasswordAlgoArgon2id, fresh) {
		t.Fatal("freshly encoded argon2id hash should not need rehash")
	}
	// A legacy hash always needs rehash.
	if !PasswordNeedsRehash("default", legacyPBKDF2Hash("password", "0123456789abcdef")) {
		t.Fatal("legacy pbkdf2 hash should need rehash")
	}
	// Correct prefix but weaker params than the current defaults.
	weak := "$argon2id$v=19$m=1024,t=1,p=1$c2FsdHNhbHRzYWx0$a2V5a2V5a2V5a2V5a2V5a2V5a2V5a2V5"
	if !PasswordNeedsRehash(PasswordAlgoArgon2id, weak) {
		t.Fatal("argon2id hash with weaker params should need rehash")
	}
	// A correct hash but stale algo column still needs rehash so the column is fixed.
	if !PasswordNeedsRehash("default", fresh) {
		t.Fatal("stale algo column should need rehash")
	}
	// Malformed hashes should be reported as needing rehash rather than panicking.
	if !PasswordNeedsRehash(PasswordAlgoArgon2id, "garbage") {
		t.Fatal("malformed hash should need rehash")
	}
}

func TestPasswordAlgoForHash(t *testing.T) {
	if got := PasswordAlgoForHash(EncodePassWord("password")); got != PasswordAlgoArgon2id {
		t.Fatalf("PasswordAlgoForHash(argon2id) = %q, want %q", got, PasswordAlgoArgon2id)
	}
	if got := PasswordAlgoForHash(legacyPBKDF2Hash("password", "0123456789abcdef")); got != "default" {
		t.Fatalf("PasswordAlgoForHash(legacy) = %q, want default", got)
	}
	if got := PasswordAlgoForHash(""); got != "default" {
		t.Fatalf("PasswordAlgoForHash(empty) = %q, want default", got)
	}
}

func TestConstantTimeStringEqual(t *testing.T) {
	if !constantTimeStringEqual("abc", "abc") {
		t.Fatal("equal strings should compare equal")
	}
	if constantTimeStringEqual("abc", "abd") {
		t.Fatal("different same-length strings should not compare equal")
	}
	if constantTimeStringEqual("abc", "abcd") {
		t.Fatal("different length strings should not compare equal")
	}
}
