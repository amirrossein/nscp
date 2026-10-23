package credential

import "testing"

func TestMemStore(t *testing.T) {
	m := NewMemStore()
	if s, err := m.LoadPassword("nscp:x"); s != "" || err != nil {
		t.Fatalf("missing entry: got %q, %v", s, err)
	}
	m.SavePassword("nscp:x", "pw")
	m.SavePassphrase("nscp:x", "pp")
	if s, _ := m.LoadPassword("nscp:x"); s != "pw" {
		t.Fatalf("got %q", s)
	}
	if s, _ := m.LoadPassphrase("nscp:x"); s != "pp" {
		t.Fatalf("got %q", s)
	}
	m.Delete("nscp:x")
	if s, _ := m.LoadPassphrase("nscp:x"); s != "" {
		t.Fatalf("got %q after delete", s)
	}
}
