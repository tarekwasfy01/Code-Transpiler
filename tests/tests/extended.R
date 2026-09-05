x <- c(12, 5, 8, 21, 3, 17, 9, 14)
y <- c(2, 4, 6, 8, 10, 12, 14, 16)
print(x)
print(x + y)
print(sum(x))
print(mean(x))
print(sort(x))
print(x > 10)

score <- 82
if (score >= 80) {
    result <- "good"
} else {
    result <- "needs improvement"
}
print(result)

counter <- 0
for (i in 1:10) {
    counter <- counter + i
}
print(counter)

n <- 1
while (n < 100) {
    n <- n * 2
}
print(n)

square <- function(value) {
    return(value * value)
}
print(square(12))

m <- matrix(c(1,2,3,4,5,6), nrow=2, ncol=3)
print(m)
print(t(m))
print(toupper("R2Cpp"))
print(cumsum(c(1,2,3,4,5)))
print(sin(c(0,0.5,1.0)))
