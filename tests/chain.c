#define _GNU_SOURCE
#include <assert.h>
#include <dlfcn.h>
#include <stdio.h>
#include <stdlib.h>

struct hook {
    void *(*install)(void *);
    void *(*remove)(void *);
    int (*calls)(void);
    void *function;
    void *(*replace)(void *, void *);
};

static struct hook load(int digit) {
    char path[32];
    snprintf(path, sizeof(path), "./libhook%d.so", digit);
    void *handle = dlopen(path, RTLD_NOW | RTLD_LOCAL);
    assert(handle);
    struct hook result = {
        dlsym(handle, "chain_install"), dlsym(handle, "chain_remove"),
        dlsym(handle, "chain_calls"), dlsym(handle, "chain_hook"),
        dlsym(handle, "chain_replace"),
    };
    assert(result.install && result.remove && result.calls && result.function && result.replace);
    return result;
}

int main(int argc, char **argv) {
    assert(argc == 5);
    int warm = atoi(argv[2]), order = atoi(argv[3]), preload = atoi(argv[4]);
    // GLOBAL lets the preload's RTLD_NEXT find the fixture's stock function.
    void *library = dlopen("./libchain.so", (preload ? RTLD_GLOBAL : RTLD_LOCAL) | (atoi(argv[1]) ? RTLD_NOW : RTLD_LAZY));
    assert(library);
    int (*call)(int) = dlsym(library, "chain_call");
    int (*stock_calls)(void) = dlsym(library, "chain_stock_calls");
    void *stock = dlsym(preload ? RTLD_DEFAULT : library, "chain_value");
    assert(call && stock && stock_calls);
    int expected = preload ? 101 : 1;
    if (warm) assert(call(0) == expected);
    struct hook hooks[3];
    int digits[3] = {order / 100, order / 10 % 10, order % 10};
    for (int i = 0; i < 3; i++) {
        hooks[i] = load(digits[i]);
        if (!i) assert(!hooks[i].replace(library, stock));
        assert(hooks[i].install(library) == (i ? hooks[i-1].function : stock));
        expected = expected * 10 + digits[i];
    }
    assert(call(0) == expected);
    for (int i = 0; i < 3; i++) assert(hooks[i].calls() == 1);
    assert(stock_calls() == warm + 1);
    // A duplicate must not make a hook call itself.
    assert(!hooks[2].install(library));
    assert(call(0) == expected);
    for (int i = 0; i < 3; i++) assert(hooks[i].calls() == 2);
    assert(stock_calls() == warm + 2);
    // Undo in reverse order, as initialization rollback does.
    for (int i = 2; i >= 0; i--) {
        assert(hooks[i].remove(library) == hooks[i].function);
        expected /= 10;
        assert(call(0) == expected);
    }
    assert(stock_calls() == warm + 5);
    printf("PASS binding=%s warm=%d order=%d preload=%d\n", atoi(argv[1]) ? "now" : "lazy", warm, order, preload);
}
