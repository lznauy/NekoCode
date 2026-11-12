package runner

import (
	"context"
	tools "nekocode/bot/extension/tool"
	"nekocode/bot/extension/tool/runtime/core"
	"nekocode/bot/extension/tool/runtime/permission"
	"nekocode/bot/extension/tool/runtime/workspace"
)

func (e *Executor) PreparePreviews(calls []core.ToolCallItem) {
	e.PreparePreviewsContext(context.Background(), calls)
}

// PreparePreviewsContext detaches each call's arguments before storing authorized
// previews. Callers must use the updated slice rather than earlier map aliases.
func (e *Executor) PreparePreviewsContext(ctx context.Context, calls []core.ToolCallItem) {
	for i := range calls {
		calls[i].Args = cloneToolArgs(calls[i].Args)
		calls[i].EffectiveArgs = cloneToolArgs(calls[i].EffectiveArgs)
		if calls[i].Args == nil {
			calls[i].Args = map[string]any{}
		}
		preview, allowed := e.authorizedPreview(ctx, calls[i])
		if allowed && preview != "" {
			calls[i].Args["_preview"] = preview
		}
	}
}

func (e *Executor) emitPreview(ctx context.Context, call core.ToolCallItem) {
	preview, allowed := e.authorizedPreview(ctx, call)
	if !allowed || preview == "" {
		return
	}
	e.fnMu.RLock()
	pfn := e.previewFn
	e.fnMu.RUnlock()
	if pfn != nil {
		pfn(call.ID, call.Name, call.Args, preview, "")
	}
}

// An unbound _preview string is not a cache of file authority. Registered
// previews are regenerated after checks, including when arguments or policy
// changed. For non-file tools without a registered preview, retain their supplied
// display text only after the same authorization checks.
func (e *Executor) authorizedPreview(ctx context.Context, call core.ToolCallItem) (string, bool) {
	supplied, _ := call.Args["_preview"].(string)
	delete(call.Args, "_preview")
	if ctx.Err() != nil {
		return "", false
	}
	policy := e.permissionSnapshot()
	var entry tools.Entry
	if e.registry != nil {
		var err error
		entry, err = e.registry.Lookup(call.Name)
		if err != nil {
			return "", false
		}
		if e.checkToolPolicy(call.Name, entry) != nil {
			return "", false
		}
		if call.EffectiveName == "" && entry.ResolveTarget != nil {
			if target, ok := entry.ResolveTarget(call.Args); ok {
				call.EffectiveName = target.Name
				call.EffectiveArgs = target.Args
			}
		}
	}
	checked := call
	checked.Args = e.lexicalPreviewArgs(policy, call.Name, call.Args)
	if call.EffectiveName != "" {
		checked.EffectiveArgs = e.lexicalPreviewArgs(policy, call.EffectiveName, call.EffectiveArgs)
	}
	if e.previewDenied(policy, checked) {
		return "", false
	}
	var sourceBindings []boundFileTarget
	var ok bool
	checked.Args, sourceBindings, ok = e.resolvePreviewFiles(checked.Name, checked.Args)
	if !ok {
		return "", false
	}
	if checked.EffectiveName != "" {
		var effectiveBindings []boundFileTarget
		checked.EffectiveArgs, effectiveBindings, ok = e.resolvePreviewFiles(checked.EffectiveName, checked.EffectiveArgs)
		if !ok {
			return "", false
		}
		sourceBindings = append(sourceBindings, effectiveBindings...)
	}
	policyCall, bindings, err := bindFilePolicyCall(checked)
	if err != nil {
		return "", false
	}
	bindings = append(bindings, sourceBindings...)
	if checked.EffectiveName != "" {
		effectivePolicy, effectiveBindings, err := bindFilePolicyCall(core.ToolCallItem{Name: checked.EffectiveName, Args: checked.EffectiveArgs})
		if err != nil {
			return "", false
		}
		policyCall.EffectiveArgs = effectivePolicy.Args
		bindings = append(bindings, effectiveBindings...)
	}
	if e.previewDenied(policy, policyCall) || !policy.current(e) {
		return "", false
	}
	if err := validateFileTargetBinding(bindings); err != nil {
		return "", false
	}
	preview := ""
	if entry.Preview != nil {
		preview = entry.Preview(e.toolContext(ctx), checked.Args)
	} else {
		_, directFile := fileToolAccess(call.Name)
		_, effectiveFile := fileToolAccess(call.EffectiveName)
		if !directFile && !effectiveFile {
			preview = supplied
		}
	}
	if ctx.Err() != nil || !policy.current(e) {
		return "", false
	}
	if err := validateFileTargetBinding(bindings); err != nil {
		return "", false
	}
	if preview != "" && call.Args != nil {
		call.Args["_preview"] = preview
	}
	return preview, true
}

// Normalize a policy copy while leaving the proxy's executable protocol intact.
func (e *Executor) lexicalPreviewArgs(policy permissionSnapshot, name string, args map[string]any) map[string]any {
	checked := cloneToolArgs(args)
	for _, target := range permission.FileToolTargets(name, args) {
		checked = permission.SetFileTarget(checked, target, permission.LexicalPath(target.Path, e.workspace.Primary(), policy.home))
	}
	return checked
}

func (e *Executor) resolvePreviewFiles(name string, args map[string]any) (map[string]any, []boundFileTarget, bool) {
	// Image inputs require the media tool's no-follow reader and content-bound
	// approval. There is no generic file-content preview for that entry point.
	if name == "image_understand" {
		return args, nil, false
	}
	access, _ := fileToolAccess(name)
	var bindings []boundFileTarget
	for _, target := range permission.FileToolTargets(name, args) {
		if target.Path == "" {
			return args, nil, false
		}
		var safe string
		var allowed bool
		var err error
		if access == workspace.AccessReadWrite {
			safe, _, allowed, err = e.workspace.CheckWrite(target.Path)
		} else {
			safe, _, allowed, err = e.workspace.CheckRead(target.Path)
		}
		if err != nil || !allowed {
			return args, nil, false
		}
		if safe != target.Path {
			bindings = append(bindings, boundFileTarget{path: target.Path, canonical: safe})
		}
		args = permission.SetFileTarget(args, target, safe)
	}
	return args, bindings, true
}

func (e *Executor) previewDenied(policy permissionSnapshot, call core.ToolCallItem) bool {
	if policy.engine == nil {
		return true
	}
	denied := func(name string, args map[string]any) bool {
		_, hit := policy.engine.DenyRule(name, permission.BuildCallInfo(name, args, policy.workspace, policy.home))
		return hit
	}
	if denied(call.Name, call.Args) {
		return true
	}
	return call.EffectiveName != "" && denied(call.EffectiveName, call.EffectiveArgs)
}
