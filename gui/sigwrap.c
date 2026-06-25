// sigwrap.c — LD_PRELOAD 包装器：拦截所有 sigaction(2) 调用，自动注入 SA_ONSTACK。
//
// 背景：Go 1.24+ 运行时要求非 Go 代码注册的信号处理器必须带 SA_ONSTACK 标志，
// 否则收到信号时直接 panic。WebKit JSC 的 JIT 会注册不带此标志的 SIGSEGV/SIGBUS，
// 即使设置 JSC_useJIT=false，JSC 仍会在 WebView 初始化后惰性注册信号处理器。
//
// 此包装器在 sigaction(2) 系统调用层面透明注入 SA_ONSTACK，无需修改 Go 或 WebKit 代码。
//
// 编译：gcc -shared -fPIC -o libsigwrap.so sigwrap.c -ldl
// 使用：LD_PRELOAD=./libsigwrap.so ./nekocode-gui

#define _GNU_SOURCE
#include <signal.h>
#include <dlfcn.h>
#include <stdio.h>

static int (*real_sigaction)(int, const struct sigaction *, struct sigaction *) = NULL;

int sigaction(int signum, const struct sigaction *act, struct sigaction *oldact) {
	if (!real_sigaction) {
		real_sigaction = dlsym(RTLD_NEXT, "sigaction");
		if (!real_sigaction) {
			fprintf(stderr, "sigwrap: dlsym failed: %s\n", dlerror());
			return -1;
		}
	}

	if (act && !(act->sa_flags & SA_ONSTACK)) {
		struct sigaction mod = *act;
		mod.sa_flags |= SA_ONSTACK;
		return real_sigaction(signum, &mod, oldact);
	}

	return real_sigaction(signum, act, oldact);
}
