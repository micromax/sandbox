// Rust Example
fn is_prime(n: u64) -> bool {
    if n <= 1 { return false; }
    for i in 2..=((n as f64).sqrt() as u64) {
        if n % i == 0 { return false; }
    }
    true
}

fn main() {
    let primes: Vec<u64> = (1..50).filter(|&x| is_prime(x)).collect();

    println!("=== Rust Language Pack ===");
    println!("Primes under 50: {:?}", primes);
    println!("Total count    : {}", primes.len());
    println!("Status: OK");
}
