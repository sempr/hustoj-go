#include <iostream>
#include <string>
#include <vector>

int main() {
    std::string mode;
    if (!(std::cin >> mode)) return 0;

    if (mode == "first") {
        int n;
        std::cin >> n;
        std::vector<int> p(n);
        int pos = 1;
        for (int i = 0; i < n; ++i) {
            std::cin >> p[i];
            if (p[i] == n) pos = i + 1;
        }
        int x = pos - 1;
        std::string s(4, 'a');
        for (int i = 3; i >= 0; --i) {
            s[i] = char('a' + x % 26);
            x /= 26;
        }
        std::cout << s << std::endl;
    } else {
        std::string s;
        std::cin >> s;
        int x = 0;
        for (char c : s) x = x * 26 + (c - 'a');
        std::cout << x + 1 << std::endl;
    }
    return 0;
}
