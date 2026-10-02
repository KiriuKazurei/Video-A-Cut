package service

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/KiriuKazurei/Video-A-Cut/control-plane/internal/model"
	"github.com/KiriuKazurei/Video-A-Cut/control-plane/internal/preparation"
)

func (s *Service) DiagnoseProvider(ctx context.Context, p preparation.Provider, apiKey, operation string) (preparation.ProviderDiagnostic, error) {
	if operation != "test" && operation != "models" {
		return preparation.ProviderDiagnostic{}, model.ErrArgument
	}
	if err := p.Validate(operation == "test"); err != nil {
		return preparation.ProviderDiagnostic{}, fmt.Errorf("provider configuration: %v: %w", err, model.ErrArgument)
	}
	if p.APIFormat == "" {
		return preparation.ProviderDiagnostic{}, fmt.Errorf("select OpenAI, Anthropic or Gemini format: %w", model.ErrArgument)
	}
	if apiKey == "" {
		apiKey = os.Getenv(p.TokenEnv)
	}
	apiKey = strings.TrimSpace(apiKey)
	if apiKey == "" {
		return preparation.ProviderDiagnostic{Code: "missing_credentials", Message: "请输入本次测试使用的 API key，或在启动环境中设置凭证变量"}, nil
	}
	if len(apiKey) > 4096 || strings.ContainsAny(apiKey, "\r\n\x00") {
		return preparation.ProviderDiagnostic{}, model.ErrArgument
	}
	return (preparation.DiagnosticClient{}).Run(ctx, p, apiKey, operation), nil
}
