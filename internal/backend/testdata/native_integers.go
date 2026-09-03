package main

import "fmt"

func bump(x uint64) uint64 { return x + uint64(1) }
func main() {
	var big uint64 = 9007199254740993
	fmt.Println(big)
	fmt.Println(bump(big))
	var maximum uint64 = 18446744073709551615
	fmt.Println(maximum + uint64(1))
	var signed int64 = 9223372036854775807
	fmt.Println(signed + int64(1))
	var negative int8 = -1
	fmt.Println(uint16(negative))
	var byteValue uint8 = 255
	fmt.Println(int64(byteValue))
	fmt.Println(byteValue * uint8(2))
	fmt.Println(-negative)
	fmt.Println(^byteValue)
	if signed > int64(-1) {
		fmt.Println("signed order")
	}
	if maximum > big {
		fmt.Println("unsigned order")
	}
	var zero uint32
	for zero < uint32(3) {
		fmt.Println(zero)
		zero = zero + uint32(1)
	}
}
