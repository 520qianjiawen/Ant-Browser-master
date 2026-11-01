package browser

import (
	"crypto/md5"
	"encoding/hex"
	"os"
	"path/filepath"
	"testing"
)

func TestVerifyCoreArchiveMD5(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "archive.zip")
	data := []byte("test-data-for-archive-md5")
	if err := os.WriteFile(p, data, 0o644); err != nil {
		t.Fatal(err)
	}

	h := md5.New()
	h.Write(data)
	expected := hex.EncodeToString(h.Sum(nil))

	match, actual, err := verifyCoreArchiveMD5(p, expected)
	if err != nil || !match || actual != expected {
		t.Fatalf("verifyCoreArchiveMD5 failed: match=%v, actual=%s, err=%v", match, actual, err)
	}

	match, _, err = verifyCoreArchiveMD5(p, "wrong-hash")
	if err != nil || match {
		t.Fatalf("expected mismatch, got match=%v, err=%v", match, err)
	}
}
