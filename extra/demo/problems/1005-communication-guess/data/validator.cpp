// 通讯题校验器：偶奇子集的双射（run-twice / spj=4）。
// argv[1] = 本轮输入；argv[2] = 原始第一轮输入（S），作为答案；
// argv[3] = 工作目录；玩家本轮输出走 stdin。
#include <fstream>
#include <iostream>
#include <sstream>
#include <string>
#include <vector>
#include <set>

static bool parseSet(const std::string& s, int& n, std::vector<int>& v, std::string& err) {
    std::istringstream in(s);
    if (!(in >> n)) { err = "missing n"; return false; }
    if (n < 1 || n > 50) { err = "n out of range"; return false; }
    int k;
    if (!(in >> k)) { err = "missing count"; return false; }
    if (k < 0 || k > n) { err = "bad count"; return false; }
    v.clear(); v.reserve(k);
    std::set<int> seen;
    int prev = 0;
    for (int i = 0; i < k; i++) {
        int x;
        if (!(in >> x)) { err = "missing element"; return false; }
        if (x < 1 || x > n) { err = "element out of range"; return false; }
        if (!seen.insert(x).second || (i > 0 && x <= prev)) {
            err = "elements must be distinct and ascending";
            return false;
        }
        v.push_back(x);
        prev = x;
    }
    int extra;
    if (in >> extra) { err = "extra tokens"; return false; }
    return true;
}

static bool setsEqual(const std::vector<int>& a, const std::vector<int>& b) {
    return a.size() == b.size() && std::equal(a.begin(), a.end(), b.begin());
}

static std::vector<int> toggleN(int n, const std::vector<int>& in) {
    std::vector<int> out;
    out.reserve(in.size() + 1);
    bool have = false;
    for (int x : in) {
        if (x == n) have = true;
        else out.push_back(x);
    }
    if (!have) out.push_back(n);
    // out already ascending because n is the largest possible element
    return out;
}

static std::string readFile(const char* path) {
    std::ifstream f(path);
    std::ostringstream ss;
    ss << f.rdbuf();
    return ss.str();
}

int main(int argc, char** argv) {
    if (argc < 4) {
        std::cerr << "usage: validator <input> <answer> <workdir>\n";
        return 2;
    }

    std::string inText = readFile(argv[1]);
    std::string ansText = readFile(argv[2]);
    std::string workdir = argv[3];

    int nIn, nAns;
    std::vector<int> inSet, ansSet;
    std::string err;
    if (!parseSet(inText, nIn, inSet, err)) {
        std::cerr << "input parse error: " << err << "\n";
        return 2;
    }
    if (!parseSet(ansText, nAns, ansSet, err)) {
        std::cerr << "answer parse error: " << err << "\n";
        return 2;
    }
    if (nAns != nIn) {
        std::cerr << "n mismatch between input and answer\n";
        return 2;
    }
    int n = nIn;

    // read player output
    std::string outText;
    {
        std::ostringstream ss;
        ss << std::cin.rdbuf();
        outText = ss.str();
    }
    int nOut;
    std::vector<int> outSet;
    if (!parseSet(outText, nOut, outSet, err)) {
        std::cerr << "output parse error: " << err << "\n";
        return 1;
    }
    if (nOut != n) {
        std::cerr << "output n must equal " << n << "\n";
        return 1;
    }

    // round detection: if current input matches the original X (argv[2]),
    // this is the first pass; otherwise the second pass.
    bool firstRound = setsEqual(inSet, ansSet);

    if (firstRound) {
        // expected output is toggle(first_input)
        std::vector<int> expected = toggleN(n, inSet);
        if (!setsEqual(outSet, expected)) {
            std::cerr << "first pass result is not the required bijection\n";
            return 1;
        }
        if ((int)outSet.size() % 2 == (int)inSet.size() % 2) {
            std::cerr << "parity of count must change\n";
            return 1;
        }
        // write canonical form of validated output for the second pass
        std::ofstream nf(workdir + "/nextpass.in");
        nf << n << " " << outSet.size() << "\n";
        for (size_t i = 0; i < outSet.size(); i++) {
            if (i) nf << " ";
            nf << outSet[i];
        }
        nf << "\n";
        if (!nf) {
            std::cerr << "failed to write nextpass.in\n";
            return 2;
        }
        return 42;
    }

    // second round: output must recover the original input (answer file)
    if (!setsEqual(outSet, ansSet)) {
        std::cerr << "second pass failed to recover original set\n";
        return 1;
    }
    // parity sanity: input (Y) and output (X) must differ in parity
    if ((int)outSet.size() % 2 == (int)inSet.size() % 2) {
        std::cerr << "parity invariant violated\n";
        return 1;
    }
    return 42;
}
