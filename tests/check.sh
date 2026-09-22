#!/bin/sh
# Run on glibc Linux on x86, x86-64 or ARM32 with a native C compiler. All outputs stay in a temporary directory.
set -eu
src=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
out=$(mktemp -d)
trap 'rm -rf "$out"' EXIT HUP INT TERM
cc=${CC:-cc}
cd "$out"
mkdir imageformats
"$cc" -std=gnu11 -Wall -Wextra -Werror -fPIC -shared -Wl,-z,lazy -o libchain.so "$src/tests/chain-library.c"
# Initialization only needs a loadable libnickel here; no firmware is used.
cp libchain.so libnickel.so.1.0.0
for digit in 1 2 3; do
    "$cc" -std=gnu11 -Wall -Wextra -Werror -fPIC -shared -I"$src" -DNH_VERSION='"test"' -DCHAIN_DIGIT=$digit -o "libhook$digit.so" "$src/tests/chain-hook.c" "$src/nh.c" -ldl -pthread
done
"$cc" -std=gnu11 -Wall -Wextra -Werror -fPIC -shared -o libpreload.so "$src/tests/chain-preload.c" -ldl
"$cc" -std=gnu11 -Wall -Wextra -Werror -o chain-check "$src/tests/chain.c" -ldl
"$cc" -std=gnu11 -Wall -Wextra -Werror -fPIC -shared -I"$src" -DNH_VERSION='"test"' -o imageformats/libfailure.so "$src/tests/failure-hook.c" "$src/nh.c" -ldl -pthread
"$cc" -std=gnu11 -Wall -Wextra -Werror -o failure-check "$src/tests/failure.c" -ldl
LD_LIBRARY_PATH="$out" ./failure-check
for now in 0 1; do
    for warm in 0 1; do
        for order in 123 132 213 231 312 321; do
            for preload in 0 1; do
                if [ "$preload" = 1 ]; then
                    LD_PRELOAD="$out/libpreload.so" ./chain-check "$now" "$warm" "$order" "$preload"
                else
                    ./chain-check "$now" "$warm" "$order" "$preload"
                fi
            done
        done
    done
done
