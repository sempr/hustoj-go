import sys


def main():
    low, high = 1, 1000
    while low <= high:
        mid = (low + high) // 2
        print(mid, flush=True)
        resp = sys.stdin.readline()
        if not resp:
            break
        resp = resp.strip()
        if resp == "correct":
            return 0
        if resp == "too small":
            low = mid + 1
        elif resp == "too large":
            high = mid - 1
    return 1


if __name__ == "__main__":
    sys.exit(main())