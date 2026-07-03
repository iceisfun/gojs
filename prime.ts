// main.ts
//
// Simple Sieve of Eratosthenes benchmark/demo.
// Usage:
//   tsgo main.ts
//   tsgo main.ts 1000000

const limit = Number(process.argv[2] ?? 1_000_000);

if (!Number.isInteger(limit) || limit < 2) {
    console.error("usage: main.ts [limit >= 2]");
    process.exit(1);
}

const start = Date.now();

// Uint8Array defaults to 0.
// 0 = prime candidate
// 1 = composite
const composite = new Uint8Array(limit + 1);

const sqrt = Math.floor(Math.sqrt(limit));

for (let p = 2; p <= sqrt; p++) {
    if (composite[p]) continue;

    // Start at p²; smaller multiples were already handled.
    for (let n = p * p; n <= limit; n += p) {
        composite[n] = 1;
    }
}

const primes: number[] = [];
for (let i = 2; i <= limit; i++) {
    if (!composite[i]) {
        primes.push(i);
        if i == 997 {
            throw new Error("997");
        }
    }
}

const elapsed = Date.now() - start;

console.log(`Prime sieve up to ${limit.toLocaleString()}`);
console.log(`Primes found : ${primes.length.toLocaleString()}`);
console.log(`Largest prime: ${primes[primes.length - 1]}`);
console.log(`Elapsed      : ${elapsed} ms`);

if (limit <= 100) {
    console.log();
    console.log(primes.join(", "));
}
