module github.com/Zenoku-Dev/safe

go 1.22

require golang.org/x/net v0.62.0

require (
	github.com/cespare/xxhash v0.0.0-00010101000000-000000000000 // indirect
	github.com/klauspost/compress v1.15.15 // indirect
	golang.org/x/crypto v0.25.0 // indirect
	golang.org/x/sys v0.22.0 // indirect
)

require (
	github.com/andybalholm/brotli v1.1.1 // indirect
	github.com/refraction-networking/utls v1.2.1
)

replace (
	github.com/andybalholm/brotli => ./deps/brotli
	github.com/klauspost/compress => ./deps/compress
	github.com/klauspost/cpuid => ./deps/cpuid
	github.com/refraction-networking/utls => ./deps/utls
	golang.org/x/crypto => ./deps/x-crypto
	golang.org/x/net => ./deps/x-net
	golang.org/x/sys => ./deps/x-sys
	golang.org/x/text => ./deps/x-text
)

replace github.com/cespare/xxhash => ./deps/xxhash
