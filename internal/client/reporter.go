package client

// JudgeReporter 是 judge 流水线（RunStandalone）的可选进度上报钩子。
// judge 子命令不设置 reporter（nil），行为完全不变；client2 通过它把评测
// 进度与最终结果写回数据库。
type JudgeReporter interface {
	// OnPhase 在评测阶段状态变化时调用，status 为 OJ_CI / OJ_RI 等。
	OnPhase(status int) error
	// OnCompileError 在编译失败时调用，res.FinalResult 为 OJ_CE 或 OJ_SE。
	OnCompileError(res *StandaloneResult) error
	// OnFinished 在评测最终结果产生后调用（含 rawwtxt）。
	OnFinished(res *StandaloneResult) error
}

func (jc *JudgeClient) notifyPhase(status int) error {
	if jc.reporter == nil {
		return nil
	}
	return jc.reporter.OnPhase(status)
}

func (jc *JudgeClient) notifyCompileError(res *StandaloneResult) error {
	if jc.reporter == nil {
		return nil
	}
	return jc.reporter.OnCompileError(res)
}

func (jc *JudgeClient) notifyFinished(res *StandaloneResult) error {
	if jc.reporter == nil {
		return nil
	}
	return jc.reporter.OnFinished(res)
}
