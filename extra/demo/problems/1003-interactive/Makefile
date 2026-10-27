# 本地编译交互题 1003 的交互器与参考解（打包时由 extra/demo/build_data.sh 代劳）。
CXX ?= g++
CXXFLAGS ?= -O2 -static

.PHONY: all clean

all: data/spj solutions/ac

data/spj: data/spj.cc
	$(CXX) $(CXXFLAGS) $< -o $@

solutions/ac: solutions/ac.cpp
	$(CXX) $(CXXFLAGS) $< -o $@

clean:
	rm -f data/spj solutions/ac
