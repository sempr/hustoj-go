// Deliberately wrong solution: prints a*b instead of a+b -> WA on every case.
#include <iostream>
using namespace std;

int main() {
    int a, b;
    while (cin >> a >> b) {
        cout << a * b << endl;
    }
    return 0;
}