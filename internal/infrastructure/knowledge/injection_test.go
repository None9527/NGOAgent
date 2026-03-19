package knowledge

import (
	"os"
	"strings"
	"testing"
)

func TestThreeTierInjection(t *testing.T) {
	dir := t.TempDir()
	store := NewStore(dir)

	// === Save test KIs ===

	// Preference KI
	if err := store.Save(&Item{
		Title:   "广告分镜导演策划规范",
		Summary: "Media-Studio 广告视频的导演规范",
		Content: "## 导演视角核心原则\n\n1. **产品定位决定视觉叙事**\n2. **分镜策划流程**\n   - Step 1: 产品分析\n   - Step 2: 场景设计\n3. **视觉风格标准**",
		Tags:    []string{"preference", "media"},
	}); err != nil {
		t.Fatal(err)
	}

	// Regular KIs
	store.Save(&Item{
		Title:   "NGOAgent 架构设计",
		Summary: "DDD 分层、10 状态机 agent loop",
		Content: "# NGOAgent Architecture\n\n## Core Engine\n- 10-state machine\n- BehaviorGuard",
		Tags:    []string{"architecture"},
	})
	store.Save(&Item{
		Title:   "Multimodal Pipeline Fix",
		Summary: "修复 fileTagRe 正则 bug",
		Content: "# Bug Fix\n\nRoot cause: [^/] excluded / in paths.\nFix: [^>]",
		Tags:    []string{"bugfix"},
	})

	// === Test GetWithContent ===
	t.Run("GetWithContent reads overview.md", func(t *testing.T) {
		items, _ := store.List()
		var prefID string
		for _, it := range items {
			if hasTag(it.Tags, "preference") {
				prefID = it.ID
			}
		}
		if prefID == "" {
			t.Fatal("no preference KI found")
		}

		// Simulate UpdateMerge adding content
		store.UpdateMerge(prefID, "\n\n---\n\n## 追加知识\n\n这是新增的内容", "更新后摘要")

		// Get() should NOT have appended content (reads metadata.json)
		gotMeta, _ := store.Get(prefID)
		if strings.Contains(gotMeta.Content, "追加知识") {
			t.Fatal("Get() should not read overview.md appended content")
		}

		// GetWithContent() SHOULD have appended content
		gotFull, _ := store.GetWithContent(prefID)
		if !strings.Contains(gotFull.Content, "追加知识") {
			t.Fatal("GetWithContent() should read appended overview.md content")
		}
		if !strings.Contains(gotFull.Content, "产品定位决定视觉叙事") {
			t.Fatal("GetWithContent() should contain original content too")
		}
		t.Logf("✅ Get()=%d chars, GetWithContent()=%d chars", len(gotMeta.Content), len(gotFull.Content))
	})

	// === Test GeneratePreferenceKI (L0) ===
	t.Run("L0 GeneratePreferenceKI full content", func(t *testing.T) {
		prefKI := store.GeneratePreferenceKI()
		if prefKI == "" {
			t.Fatal("GeneratePreferenceKI returned empty")
		}
		if !strings.Contains(prefKI, "分镜策划流程") {
			t.Fatal("L0 should contain full original content")
		}
		if !strings.Contains(prefKI, "追加知识") {
			t.Fatal("L0 should contain appended content from overview.md")
		}
		if strings.Contains(prefKI, "NGOAgent Architecture") {
			t.Fatal("L0 should NOT contain non-preference KIs")
		}
		t.Logf("✅ PreferenceKI: %d chars", len(prefKI))
	})

	// === Test GenerateKIIndex (L2) ===
	t.Run("L2 GenerateKIIndex with paths", func(t *testing.T) {
		kiIndex := store.GenerateKIIndex()
		if kiIndex == "" {
			t.Fatal("GenerateKIIndex returned empty")
		}
		if !strings.Contains(kiIndex, "overview.md") {
			t.Fatal("L2 should contain artifact file paths")
		}
		if !strings.Contains(kiIndex, "[PREFERENCE]") {
			t.Fatal("L2 should mark preference KIs")
		}
		if !strings.Contains(kiIndex, "NGOAgent") {
			t.Fatal("L2 should contain all KIs")
		}
		if !strings.Contains(kiIndex, "Multimodal") {
			t.Fatal("L2 should contain all KIs")
		}
		// Verify paths are real files
		for _, line := range strings.Split(kiIndex, "\n") {
			if idx := strings.Index(line, "→"); idx > 0 {
				pathStr := strings.TrimSpace(line[idx+len("→"):])
				paths := strings.Split(pathStr, ", ")
				for _, p := range paths {
					p = strings.TrimSpace(p)
					if p == "" {
						continue
					}
					if _, err := os.Stat(p); err != nil {
						t.Errorf("L2 artifact path does not exist: %s", p)
					}
				}
			}
		}
		t.Logf("✅ KIIndex: %d chars\n%s", len(kiIndex), kiIndex)
	})

	// === Test backward compat aliases ===
	t.Run("Backward compat aliases", func(t *testing.T) {
		pref1 := store.GeneratePreferenceKI()
		pref2 := store.GeneratePreferenceSummaries()
		if pref1 != pref2 {
			t.Fatal("GeneratePreferenceSummaries should be alias for GeneratePreferenceKI")
		}
		idx1 := store.GenerateKIIndex()
		idx2 := store.GenerateSummaries()
		if idx1 != idx2 {
			t.Fatal("GenerateSummaries should be alias for GenerateKIIndex")
		}
		t.Log("✅ Backward compat aliases work")
	})
}
