package main

import "fmt"

func main() {
	var active bool = true
	text := "outer"
	{
		text := "inner"
		fmt.Println(text)
	}
	for active {
		fmt.Println(text)
		active = false
	}
	if !active && text == "outer" {
		fmt.Println("done")
	}
	if text == "different" {
		fmt.Println("wrong equality")
	}
	if text != "different" {
		fmt.Println("different")
	}
	if text != "outer" {
		fmt.Println("wrong inequality")
	}
}
