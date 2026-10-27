#![allow(dead_code)]
#![allow(unused_imports)]
#![allow(unused_variables)]
#![allow(unused_must_use)]
use collections::*;
use std::io::Write;
use std::*;

fn solve(a: usize, b: usize) -> String {
    if a % b == 0 {
        return (a / b).to_string();
    }

    let integer_part = (a / b).to_string();
    let mut digits: Vec<char> = Vec::new();

    let mut ex = vec![-1isize; b];
    let mut rem = a % b;

    loop {
        if rem == 0 {
            let frac: String = digits.into_iter().collect();
            return format!("{}.{}", integer_part, frac);
        }

        if ex[rem] != -1 {
            let pos = ex[rem] as usize;

            let prefix: String = digits[..pos].iter().collect();
            let cycle: String = digits[pos..].iter().collect();

            return format!("{}.{}[{}]", integer_part, prefix, cycle);
        }

        ex[rem] = digits.len() as isize;

        rem *= 10;
        digits.push(char::from(b'0' + (rem / b) as u8));
        rem %= b;
    }
}

fn test_case<W: Write>(sc: &mut input::In, out: &mut std::io::BufWriter<W>) {
  let a:usize = sc.read();
  let b:usize = sc.read();
  writeln!(out, "{}", solve(a, b));
}

fn main() {
    use std::io::Read;
    let mut s = String::new();
    std::io::stdin().read_to_string(&mut s).unwrap();
    let mut sc = input::In::new(&s);
    let out = std::io::stdout();
    let mut out = std::io::BufWriter::new(out.lock());
    let t = 1;
    let t: usize = sc.read();
    for _ in 0..t {
        test_case(&mut sc, &mut out);
    }
}

mod input {
    use std::str::FromStr;
    use std::str::SplitWhitespace;
    pub struct In<'a> {
        iter: SplitWhitespace<'a>,
    }
    impl<'a> In<'a> {
        pub fn new(s: &'a str) -> Self {
            Self {
                iter: s.split_whitespace(),
            }
        }
        pub fn read<T: FromStr>(&mut self) -> T {
            self.iter.next().unwrap().parse::<T>().ok().unwrap()
        }

        pub fn read_bytes(&mut self) -> Vec<u8> {
            self.iter.next().unwrap().bytes().collect()
        }
        pub fn read_chars(&mut self) -> Vec<char> {
            self.iter.next().unwrap().chars().collect()
        }
        pub fn read_vec<T: FromStr>(&mut self, n: usize) -> Vec<T> {
            (0..n).map(|_| self.read()).collect()
        }
    }
}
