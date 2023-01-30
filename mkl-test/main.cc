#include <mkl.h>

#include <iostream>
#include <vector>

int main() {
  std::vector<double> x{1., 2., 3.};
  std::vector<double> y(x.size(), 0);

  cblas_daxpy(x.size(), 3, x.data(), 1, y.data(), 1);

  for (const auto z : y) {
    std::cout << z << std::endl;
  }

  return 0;
}
