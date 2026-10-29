package service

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"time"
)

type DiagnosticCheck struct {
	ID       string `json:"id"`
	Label    string `json:"label"`
	Status   string `json:"status"`
	Message  string `json:"message"`
	NextStep string `json:"next_step,omitempty"`
	Version  string `json:"version,omitempty"`
}
type DiagnosticReport struct {
	SchemaVersion int               `json:"schema_version"`
	GeneratedAt   time.Time         `json:"generated_at"`
	Platform      string            `json:"platform"`
	Architecture  string            `json:"architecture"`
	Status        string            `json:"status"`
	Redacted      bool              `json:"redacted"`
	Scope         string            `json:"scope"`
	Checks        []DiagnosticCheck `json:"checks"`
}

var diagnosticVersion = regexp.MustCompile(`^(?:[vV]|Python |(?:ffmpeg|ffprobe) version [nN]?)([0-9]+\.[0-9]+(?:\.[0-9]+)?)`)

// SystemDiagnostics is a bounded, read-only, local snapshot. It never sends model
// requests or exports raw errors, command output, configuration, paths or names.
func (s *Service) SystemDiagnostics(ctx context.Context) (DiagnosticReport, error) {
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	report := DiagnosticReport{SchemaVersion: 1, GeneratedAt: time.Now().UTC(), Platform: runtime.GOOS, Architecture: runtime.GOARCH, Status: "passed", Redacted: true, Scope: "local_read_only_no_model_requests", Checks: []DiagnosticCheck{}}
	add := func(id, label, status, message, next string) {
		report.Checks = append(report.Checks, DiagnosticCheck{ID: id, Label: label, Status: status, Message: message, NextStep: next})
	}
	if err := ctx.Err(); err != nil {
		return report, err
	}
	if _, err := s.RuntimeActivity(ctx); err != nil {
		add("database", "本地数据库", "failed", "无法读取本地运行状态。", "检查数据目录权限与后台日志。")
	} else {
		add("database", "本地数据库", "passed", "运行状态可读取；未修改业务数据。", "")
	}
	_, python, rootErr := s.scriptRoot()
	if python == "" {
		python = "python"
	}
	for _, tool := range []struct {
		id, label, command string
		args               []string
	}{
		{"node", "Node.js", "node", []string{"--version"}},
		{"python", "Python", python, []string{"-B", "--version"}},
		{"ffmpeg", "FFmpeg", "ffmpeg", []string{"-version"}},
		{"ffprobe", "FFprobe", "ffprobe", []string{"-version"}},
	} {
		if err := ctx.Err(); err != nil {
			return report, err
		}
		probeCtx, stop := context.WithTimeout(ctx, 2*time.Second)
		cmd := exec.CommandContext(probeCtx, tool.command, tool.args...)
		configureMediaCommand(cmd)
		cmd.WaitDelay = time.Second
		cmd.Env = toolEnvironment()
		var out scriptOutput
		cmd.Stdout = &out
		cmd.Stderr = &out
		err := cmd.Run()
		stop()
		if err != nil {
			add("dependency."+tool.id, tool.label, "failed", "未找到可执行程序，或版本检查未能完成。", "安装所需版本并检查后台进程的工具配置与 PATH。")
			continue
		}
		version := diagnosticVersion.FindStringSubmatch(strings.TrimSpace(out.String()))
		if len(version) != 2 {
			add("dependency."+tool.id, tool.label, "warning", "程序已响应，但无法识别版本。", "检查安装的工具版本。")
			continue
		}
		status, message, next := "passed", "本机版本检查通过。", ""
		var major, minor int
		_, _ = fmt.Sscanf(version[1], "%d.%d", &major, &minor)
		if tool.id == "node" && (major < 22 || (major == 22 && minor < 12)) {
			status, message, next = "failed", "Node.js 版本低于 22.12。", "安装 Node.js 22.12 或更高受支持版本。"
		}
		if tool.id == "python" && major < 3 {
			status, message, next = "failed", "需要 Python 3。", "安装项目 requirements 对应的 Python 3 环境。"
		}
		add("dependency."+tool.id, tool.label, status, message, next)
		report.Checks[len(report.Checks)-1].Version = version[1]
	}
	if rootErr != nil {
		add("worker.resources", "Worker 资源", "warning", "尚未定位 Python/Node Worker 资源。", "检查桌面运行资源目录配置。")
	} else {
		root, _, _ := s.scriptRoot()
		ok := true
		for _, rel := range []string{"workers/node/timeline-cli/src/script-workbench.mjs", "workers/python/vac_worker/script_workbench.py"} {
			if info, err := os.Stat(filepath.Join(root, rel)); err != nil || info.IsDir() {
				ok = false
			}
		}
		if ok {
			add("worker.resources", "Worker 资源", "passed", "共享 Worker 入口文件存在。", "")
		} else {
			add("worker.resources", "Worker 资源", "failed", "共享 Worker 入口文件不完整。", "重新部署同版运行资源。")
		}
		installed := true
		for _, name := range []string{"hyperframes", "remotion", "@remotion/renderer", "@remotion/bundler"} {
			if _, err := os.Stat(filepath.Join(root, "workers/node/timeline-cli/node_modules", name, "package.json")); err != nil {
				installed = false
			}
		}
		if installed {
			add("draw.dependencies", "绘制依赖", "passed", "本地引擎依赖清单存在；尚未执行渲染。", "")
		} else {
			add("draw.dependencies", "绘制依赖", "warning", "部分本地绘制依赖未安装。", "按锁文件准备绘制依赖，并配置已有 Chromium；诊断不会下载安装。")
		}
	}
	counts, err := s.st.OnlineWorkerCounts(ctx, time.Now().UTC().Add(-90*time.Second))
	for _, role := range []struct{ id, label string }{{"ingester", "导入 Worker"}, {"recognizer", "识别 Worker"}, {"narrator", "解说 Worker"}, {"exporter", "导出 Worker"}} {
		if err != nil {
			add("worker."+role.id, role.label, "warning", "无法读取 Worker 心跳。", "检查后台状态。")
		} else if counts[role.id] > 0 {
			add("worker."+role.id, role.label, "passed", fmt.Sprintf("最近 90 秒内有 %d 个健康心跳。", counts[role.id]), "")
		} else {
			add("worker."+role.id, role.label, "warning", "最近 90 秒内没有健康心跳。", "检查对应 Worker；部分按需工具不会注册常驻心跳。")
		}
	}
	profiles, err := s.ListProcessingProfiles(ctx, 200, 0)
	if err != nil {
		add("models.configuration", "模型配置", "warning", "无法读取处理预设。", "检查本地数据库。")
	} else {
		external, missing, local, absent := 0, 0, 0, 0
		for _, p := range profiles {
			if p.External() {
				external++
				if (p.Vision.TokenEnv != "" && os.Getenv(p.Vision.TokenEnv) == "") || (p.Narration.TokenEnv != "" && os.Getenv(p.Narration.TokenEnv) == "") {
					missing++
				}
			}
			if p.Transcription != nil && p.Transcription.Local() {
				local++
				path := p.Transcription.ModelPath
				if strings.HasPrefix(path, `\\`) {
					absent++
					continue
				}
				for _, f := range []string{"model.bin", "config.json"} {
					if info, e := os.Stat(filepath.Join(path, f)); e != nil || info.IsDir() {
						absent++
						break
					}
				}
			}
		}
		if missing > 0 {
			add("models.configuration", "模型配置", "warning", "部分外部模型预设缺少凭证环境变量。", "在启动后台的环境中配置凭证；诊断不会发送请求。")
		} else if external > 0 {
			add("models.configuration", "模型配置", "not_checked", "外部模型配置与凭证引用存在；未验证服务连通性或模型质量。", "需要时在服务商设置中单独测试。")
		} else {
			add("models.configuration", "模型配置", "not_checked", "未配置外部模型；本地流程不受此项限制。", "需要云端生成时配置服务商。")
		}
		if local == 0 {
			add("models.local_asr", "本地转录模型", "not_checked", "未配置本地转录模型。", "需要离线转录时选择模型目录。")
		} else if absent > 0 {
			add("models.local_asr", "本地转录模型", "warning", "部分本地模型缺少所需文件或使用网络路径。", "检查本地模型目录；在转录设置中执行依赖检查。")
		} else {
			add("models.local_asr", "本地转录模型", "not_checked", "配置的模型文件存在；未加载模型或执行识别。", "在转录设置中执行依赖检查。")
		}
	}
	ttsCtx, stop := context.WithTimeout(ctx, 3*time.Second)
	tts, err := s.TTSCapabilities(ttsCtx, "")
	stop()
	if err == nil && tts.RuntimeReady && tts.MediaReady {
		add("tts.local", "配音环境", "passed", fmt.Sprintf("本地运行环境可用，发现 %d 个系统声音；云端配音未测试。", len(tts.SAPIVoices)), "")
	} else {
		add("tts.local", "配音环境", "warning", "配音环境未配置完整，或本机检查未完成。", "在配音页检查 Python 依赖、媒体工具与可用声音。")
	}
	if err := ctx.Err(); err != nil {
		return report, err
	}
	for _, c := range report.Checks {
		if c.Status == "failed" {
			report.Status = "failed"
			break
		}
		if c.Status == "warning" {
			report.Status = "warning"
		}
	}
	return report, nil
}
