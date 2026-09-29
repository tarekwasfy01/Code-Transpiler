#[derive(Debug, Clone, PartialEq)]
pub struct Matrix {
    pub rows: usize,
    pub cols: usize,
    pub data: Vec<f64>,
}

impl Matrix {
    pub fn new(rows: usize, cols: usize) -> Self {
        Self {
            rows,
            cols,
            data: vec![0.0; rows.saturating_mul(cols)],
        }
    }
    pub fn from_vec(rows: usize, cols: usize, data: Vec<f64>) -> Result<Self, String> {
        if rows.checked_mul(cols) != Some(data.len()) {
            return Err("matrix dimensions do not match data length".into());
        }
        Ok(Self { rows, cols, data })
    }
    pub fn at(&self, row: usize, col: usize) -> f64 {
        self.data[row * self.cols + col]
    }
    pub fn set(&mut self, row: usize, col: usize, value: f64) {
        self.data[row * self.cols + col] = value;
    }
    pub fn valid(&self) -> bool {
        self.rows.checked_mul(self.cols) == Some(self.data.len())
    }
    pub fn multiply(&self, rhs: &Matrix) -> Result<Matrix, String> {
        if self.cols != rhs.rows {
            return Err("matrix shape mismatch".into());
        }
        let mut out = Matrix::new(self.rows, rhs.cols);
        for r in 0..self.rows {
            for c in 0..rhs.cols {
                let mut sum = 0.0;
                for k in 0..self.cols {
                    sum += self.at(r, k) * rhs.at(k, c);
                }
                out.set(r, c, sum);
            }
        }
        Ok(out)
    }
}

#[derive(Debug, Clone, PartialEq)]
pub struct SparseMatrix {
    pub rows: usize,
    pub cols: usize,
    pub entries: Vec<(usize, usize, f64)>,
}
impl SparseMatrix {
    pub fn new(rows: usize, cols: usize) -> Self {
        Self {
            rows,
            cols,
            entries: Vec::new(),
        }
    }
    pub fn set(&mut self, row: usize, col: usize, value: f64) {
        if let Some(e) = self.entries.iter_mut().find(|e| e.0 == row && e.1 == col) {
            e.2 = value;
        } else if value != 0.0 {
            self.entries.push((row, col, value));
        }
    }
    pub fn at(&self, row: usize, col: usize) -> f64 {
        self.entries
            .iter()
            .find(|e| e.0 == row && e.1 == col)
            .map(|e| e.2)
            .unwrap_or(0.0)
    }
    pub fn multiply(&self, rhs: &SparseMatrix) -> Result<SparseMatrix, String> {
        if self.cols != rhs.rows {
            return Err("sparse matrix shape mismatch".into());
        }
        let mut out = SparseMatrix::new(self.rows, rhs.cols);
        for r in 0..self.rows {
            for c in 0..rhs.cols {
                let mut sum = 0.0;
                for k in 0..self.cols {
                    sum += self.at(r, k) * rhs.at(k, c);
                }
                if sum != 0.0 {
                    out.set(r, c, sum);
                }
            }
        }
        Ok(out)
    }
}
