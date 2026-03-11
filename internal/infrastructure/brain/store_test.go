package brain

import (
	"os"
	"testing"
)

func TestVersionedWrite(t *testing.T) {
	dir := t.TempDir()
	store := NewArtifactStore(dir, "test-session")

	// Write v1
	if err := store.WriteArtifact("task.md", "# V1\n- [ ] item 1", "version 1", "task"); err != nil {
		t.Fatalf("write v1: %v", err)
	}

	// Write v2 — should rotate v1 to .resolved.0
	if err := store.WriteArtifact("task.md", "# V2\n- [x] item 1\n- [ ] item 2", "version 2", "task"); err != nil {
		t.Fatalf("write v2: %v", err)
	}

	// Write v3 — should rotate v2 to .resolved.1
	if err := store.WriteArtifact("task.md", "# V3 Final", "version 3", "task"); err != nil {
		t.Fatalf("write v3: %v", err)
	}

	// Current should be v3
	content, err := store.Read("task.md")
	if err != nil {
		t.Fatalf("read current: %v", err)
	}
	if content != "# V3 Final" {
		t.Fatalf("expected V3, got: %s", content)
	}

	// Metadata version should be 3
	meta, err := store.ReadMetadata("task.md")
	if err != nil {
		t.Fatalf("read metadata: %v", err)
	}
	if meta.Version != 3 {
		t.Fatalf("expected version 3, got %d", meta.Version)
	}
	if meta.ArtifactType != "task" {
		t.Fatalf("expected type=task, got %s", meta.ArtifactType)
	}
	if meta.Summary != "version 3" {
		t.Fatalf("expected summary='version 3', got %s", meta.Summary)
	}

	// Should have 2 versions in history (v1=resolved.0, v2=resolved.1)
	versions := store.ListVersions("task.md")
	if len(versions) != 2 {
		t.Fatalf("expected 2 versions, got %v", versions)
	}

	// Version 0 should be v1 content
	v0, err := store.ReadVersion("task.md", 0)
	if err != nil {
		t.Fatalf("read version 0: %v", err)
	}
	if v0 != "# V1\n- [ ] item 1" {
		t.Fatalf("version 0 content wrong: %s", v0)
	}

	// Version 1 should be v2 content
	v1, err := store.ReadVersion("task.md", 1)
	if err != nil {
		t.Fatalf("read version 1: %v", err)
	}
	if v1 != "# V2\n- [x] item 1\n- [ ] item 2" {
		t.Fatalf("version 1 content wrong: %s", v1)
	}

	// .resolved should be latest pre-current (v2)
	resolvedPath := store.Dir() + "/task.md.resolved"
	data, err := os.ReadFile(resolvedPath)
	if err != nil {
		t.Fatalf("read .resolved: %v", err)
	}
	if string(data) != "# V2\n- [x] item 1\n- [ ] item 2" {
		t.Fatalf(".resolved wrong: %s", string(data))
	}

	t.Log("✅ Brain versioning: 3 writes → current V3, 2 history, metadata v3")
}
