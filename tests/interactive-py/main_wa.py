import sys


def main():
    while True:
        print(1, flush=True)  # always guesses 1: never correct for secret 42/73
        resp = sys.stdin.readline()
        if not resp:
            break
    return 0


if __name__ == "__main__":
    sys.exit(main())