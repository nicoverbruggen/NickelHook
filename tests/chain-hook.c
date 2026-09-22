#include "NickelHook.h"

// Exercise separate embedded copies of NickelHook without plugin startup.
struct nh NickelHook = {0};
static struct nh_info info = {.name = "chain-check"};
extern void *nh_dlhook(void *, const char *, void *);
static int (*previous)(int);
static int calls;

int chain_hook(int value) {
    calls++;
    return previous(value) * 10 + CHAIN_DIGIT;
}

void *chain_replace(void *library, void *target) {
    NickelHook.info = &info;
    return nh_dlhook(library, "chain_value", target);
}

void *chain_install(void *library) {
    void *result = chain_replace(library, chain_hook);
    if (result) previous = result;
    return result;
}

void *chain_remove(void *library) {
    return nh_dlhook(library, "chain_value", previous);
}

int chain_calls(void) {
    return calls;
}
