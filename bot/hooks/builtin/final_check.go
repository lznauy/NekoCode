package builtin

// FinalCheckHook enforces honesty of the agent's final answer against the
// ledger execution record. It replaces the former parallel applyFinalCheck
// path, unifying final-answer governance into the hook system.
//
// Three rules (checked in priority order):
//  1. missing_verification:    non-doc files modified, no passing verification,
//                              and answer does not disclose "未验证".
//  2. unsupported_test_claim:  answer claims tests passed, but ledger has no
//                              passing verification record.
//  3. unreported_tool_error:   tool errors exist, answer claims completion
//                              without mentioning any failure.
func FinalCheckHook() Hook {
	return Hook{
		Name:  "final_check",
		Point: PostTurn,
		On: func(s State) *Result {
			text := s.GetStr(StoreFinalAnswerText)
			if text == "" {
				return nil
			}

			hasNonDocMods := s.Get(StoreLedgerNonDocModified) == 1
			hasPassingVerify := s.Get(StoreLedgerVerified) == 1
			toolErrorCount := s.Get(StoreLedgerErrors)

			if hasNonDocMods && !hasPassingVerify && !mentionsUnverified(text) {
				return &Result{BlockFinal: &BlockFinal{
					Reason: "你已经修改了文件，但还没有成功验证。请先运行合适的验证命令；如果无法验证，最终回答必须明确说明未验证。",
				}}
			}

			if claimsTestsPassed(text) && !hasPassingVerify {
				return &Result{BlockFinal: &BlockFinal{
					Reason: "最终回答声称测试或验证已通过，但 ledger 中没有成功验证记录。请运行验证命令，或移除该声明。",
				}}
			}

			if toolErrorCount > 0 && claimsCompleted(text) && !mentionsFailure(text) {
				return &Result{BlockFinal: &BlockFinal{
					Reason: "存在工具错误，但最终回答没有披露。请处理错误或在最终回答中说明。",
				}}
			}

			return nil
		},
	}
}
