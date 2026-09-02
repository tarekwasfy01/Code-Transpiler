package main

import "fmt"

func caller(s string) string          { return mark(s) }
func mark(s string) string            { fmt.Println(s); return s }
func first(a string, b string) string { return a }
func probe() bool                     { fmt.Println("probe"); return true }
func mutate(s string)                 { s = "changed"; fmt.Println(s); return }
func choose(b bool, s string) string {
	if b {
		return s
	}
	return "fallback"
}
func loop(s string) string {
	active := true
	for active {
		active = false
		if s == "early" {
			return s
		}
		continue
	}
	return "after"
}
func silent()          {}
func consume(s string) { fmt.Println(s) }
func ignore(s string)  {}
func main() {
	s := "original"
	mutate(s)
	fmt.Println(s)
	fmt.Println(first(caller("left"), mark("right")))
	if false && probe() {
		fmt.Println("wrong")
	}
	if true || probe() {
		fmt.Println("skipped")
	}
	if true && probe() {
		fmt.Println("evaluated")
	}
	fmt.Println(choose(true, "chosen"))
	fmt.Println(choose(false, "unused"))
	if mark("binary-left") != mark("binary-right") {
		fmt.Println("unequal")
	}
	fmt.Println(loop("early"))
	fmt.Println(loop("late"))
	silent()
	consume("fallthrough")
	ignore(mark("unused effect"))
	if false && mark("wrong left") == mark("wrong right") {
		fmt.Println("wrong")
	}
}
