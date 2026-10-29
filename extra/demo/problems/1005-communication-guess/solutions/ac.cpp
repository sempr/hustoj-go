#include <iostream>

int main() {
    long long lo = 0, hi = 0;
    if (std::cin >> lo >> hi) {
        std::cout << lo + (hi - lo) / 2 << std::endl;
    }
    return 0;
}
