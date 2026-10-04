// ac_guess.cpp - 正确玩家：二分查找 secret（interactive spj=16 测试用）。
#include <iostream>
#include <string>

int main() {
    int low = 1, high = 1000;
    while (low <= high) {
        int mid = (low + high) / 2;
        std::cout << mid << std::endl;

        std::string resp;
        if (!std::getline(std::cin, resp)) break;
        if (resp == "correct") return 0;
        if (resp == "too small") low = mid + 1;
        else if (resp == "too large") high = mid - 1;
    }
    return 0;
}