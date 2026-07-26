// FizzBuzz: 1から15まで数え、3の倍数でFizz、5の倍数でBuzz、
// 両方の倍数でFizzBuzzと表示する……はずのプログラム。
// エージェントに「バグを直して」と頼んでみよう。
package main

import "fmt"

func main() {
	for i := 1; i <= 15; i++ {
		fmt.Println(fizzbuzz(i))
	}
}

func fizzbuzz(n int) string {
	if n%3 == 0 {
		return "Fizz"
	}
	if n%5 == 0 {
		return "Buzz"
	}
	if n%15 == 0 {
		return "FizzBuzz"
	}
	return fmt.Sprint(n)
}
