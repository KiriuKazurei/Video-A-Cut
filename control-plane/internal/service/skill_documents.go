package service

import (
	"encoding/json"
	"fmt"
	"github.com/KiriuKazurei/Video-A-Cut/control-plane/internal/skill"
	"os"
	"path/filepath"
)

func (s *Service) writeSkillSummary(r *skill.Run) error {
	dir, err := s.harnessDirectory(filepath.Join("projects", r.ProjectID, "summaries", r.ID))
	if err != nil {
		return err
	}
	var value struct {
		Model     string `json:"model"`
		Narrative string `json:"narrative"`
		Timeline  []struct {
			Media       string  `json:"media_id"`
			Start       float64 `json:"start"`
			End         float64 `json:"end"`
			Description string  `json:"description"`
			Confidence  string  `json:"confidence"`
		} `json:"timeline"`
	}
	if err = json.Unmarshal(r.Summary, &value); err != nil {
		return err
	}
	raw, err := json.MarshalIndent(map[string]any{"project_id": r.ProjectID, "run_id": r.ID, "visible_set_sha": r.VisibleSHA, "skill_sha256": r.SkillSHA, "frames": r.Frames, "summary": r.Summary}, "", "  ")
	if err != nil {
		return err
	}
	jsonFile := filepath.Join(dir, fmt.Sprintf("summary-%d.json", r.Steps))
	if err = os.WriteFile(jsonFile, raw, 0600); err != nil {
		return err
	}
	markdown := fmt.Sprintf("# 视频内容大意\n\n项目：%s\n运行：%s\n模型：%s\n可见集：%s\n\n%s\n\n## 时间线摘要\n", r.ProjectID, r.ID, value.Model, r.VisibleSHA, value.Narrative)
	for _, row := range value.Timeline {
		markdown += fmt.Sprintf("\n- %s %.3f–%.3f 秒（%s）：%s\n", row.Media, row.Start, row.End, row.Confidence, row.Description)
	}
	md := filepath.Join(dir, fmt.Sprintf("summary-%d.md", r.Steps))
	if err = os.WriteFile(md, []byte(markdown), 0600); err != nil {
		return err
	}
	if _, err = s.registerSkillArtifact(r, jsonFile, "content-summary.json", "summary", 0); err != nil {
		return err
	}
	_, err = s.registerSkillArtifact(r, md, "content-summary.md", "summary", 0)
	return err
}
