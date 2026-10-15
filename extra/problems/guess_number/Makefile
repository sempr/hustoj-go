CXX ?= g++
CXXFLAGS ?= -O2 -static

.PHONY: all clean

all: data/spj solution/ac

data/spj: data/spj.cc
	$(CXX) $(CXXFLAGS) $< -o $@

solution/ac: solution/ac.cpp
	$(CXX) $(CXXFLAGS) $< -o $@

clean:
	rm -f data/spj solution/ac
