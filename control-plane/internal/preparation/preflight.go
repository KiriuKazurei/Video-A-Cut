package preparation

import "context"

var requiredRuntimeChecks = []string{"ffmpeg", "ffprobe", "python_worker", "node_exporter", "tts_voice", "credentials", "provider_binding"}

type Engine struct{ Probe RuntimeProbe }

// Check performs structural validation and delegates local observations only.
// Launch and external-endpoint authorization deliberately remain blocked in
// this skeleton, including when an injected probe reports all tools available.
func (e Engine) Check(ctx context.Context, p Profile) (Report, error) {
	if err := ctx.Err(); err != nil {
		return Report{}, err
	}
	fingerprint, err := p.Fingerprint()
	if err != nil {
		return Report{}, err
	}
	r := Report{SchemaVersion: 1, ProfileID: p.ProfileID, ProfileRevision: p.Revision, ProfileSHA256: fingerprint, Status: "blocked", Checks: []Check{{Code: "profile_schema", Status: "passed", Message: "预设结构及预算通过；尚未绑定运行任务"}}}
	observed := map[string]Check{}
	if e.Probe != nil {
		checks, err := e.Probe.Probe(ctx, p)
		if err != nil {
			r.Checks = append(r.Checks, Check{Code: "runtime_probe", Status: "blocked", Message: "本地能力探测失败；未调用模型"})
		} else {
			for _, check := range checks {
				observed[check.Code] = check
			}
		}
	}
	for _, code := range requiredRuntimeChecks {
		check, ok := observed[code]
		if !ok {
			check = Check{Code: code, Status: "not_checked", Message: "本地能力探测适配器尚未接入"}
		}
		if check.Status != "passed" && check.Status != "blocked" && check.Status != "not_checked" {
			check.Status = "blocked"
			check.Message = "无效能力探测状态"
		}
		r.Checks = append(r.Checks, check)
	}
	if p.Vision.External() || p.Narration.External() {
		r.Checks = append(r.Checks, Check{Code: "external_authorization", Status: "blocked", Message: "需控制面持久化的外发许可；预设不是授权"})
	}
	r.Checks = append(r.Checks, Check{Code: "prepared_launch", Status: "blocked", Message: "预设绑定启动尚未实现；此接口不会创建流程或任务"})
	return r, nil
}
