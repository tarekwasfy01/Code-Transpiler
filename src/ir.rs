#[derive(Debug, Clone, PartialEq)]
pub struct Program {
    pub structs: Vec<StructDef>,
    pub functions: Vec<Function>,
}

#[derive(Debug, Clone, PartialEq)]
pub struct StructDef {
    pub name: String,
    pub fields: Vec<StructField>,
}

#[derive(Debug, Clone, PartialEq)]
pub struct StructField {
    pub name: String,
    pub ty: Type,
}

#[derive(Debug, Clone, PartialEq)]
pub struct Function {
    pub name: String,
    pub params: Vec<Param>,
    pub return_type: Option<Type>,
    pub body: Vec<Stmt>,
}

#[derive(Debug, Clone, PartialEq)]
pub struct Param {
    pub name: String,
    pub ty: Type,
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub enum Type {
    Unit,
    Bool,
    String,
    Int,
    Int8,
    Int16,
    Int32,
    Int64,
    UInt,
    UInt8,
    UInt16,
    UInt32,
    UInt64,
    Float32,
    Float64,
    Slice(Box<Type>),
    Map(Box<Type>, Box<Type>),
    Tuple(Vec<Type>),
    Named(String),
}

#[derive(Debug, Clone, PartialEq)]
pub enum Stmt {
    Let {
        name: String,
        ty: Option<Type>,
        value: Expr,
        mutable: bool,
    },
    MultiLet {
        names: Vec<String>,
        value: Expr,
        mutable: bool,
    },
    Assign {
        target: String,
        op: String,
        value: Expr,
    },
    IndexAssign {
        collection: Expr,
        index: Expr,
        op: String,
        value: Expr,
        map_value_type: Option<Type>,
    },
    MultiAssign {
        targets: Vec<String>,
        value: Expr,
    },
    Expr(Expr),
    Print {
        newline: bool,
        args: Vec<Expr>,
    },
    Return(Option<Expr>),
    If {
        cond: Expr,
        then_body: Vec<Stmt>,
        else_body: Vec<Stmt>,
    },
    While {
        cond: Expr,
        body: Vec<Stmt>,
    },
    ForEach {
        value: String,
        iter: Expr,
        body: Vec<Stmt>,
    },
    Switch {
        value: Option<Expr>,
        cases: Vec<SwitchCase>,
        default: Vec<Stmt>,
    },
    Break,
    Continue,
}

#[derive(Debug, Clone, PartialEq)]
pub struct SwitchCase {
    pub values: Vec<Expr>,
    pub body: Vec<Stmt>,
}

#[derive(Debug, Clone, PartialEq)]
pub enum Expr {
    Ident(String),
    Int(String),
    Float(String),
    String(String),
    Bool(bool),
    Nil,
    Unary {
        op: String,
        value: Box<Expr>,
    },
    Binary {
        left: Box<Expr>,
        op: String,
        right: Box<Expr>,
    },
    Call {
        function: String,
        args: Vec<Expr>,
    },
    Index {
        value: Box<Expr>,
        index: Box<Expr>,
    },
    Array {
        items: Vec<Expr>,
        element_type: Option<Box<Type>>,
    },
    Map {
        key_type: Box<Type>,
        value_type: Box<Type>,
        entries: Vec<(Expr, Expr)>,
    },
    MapIndex {
        value: Box<Expr>,
        index: Box<Expr>,
        value_type: Box<Type>,
    },
    MapLookupOk {
        value: Box<Expr>,
        index: Box<Expr>,
        value_type: Box<Type>,
    },
    Tuple(Vec<Expr>),
    StructLiteral {
        ty: String,
        fields: Vec<(String, Expr)>,
        positional: Vec<Expr>,
    },
    Raw(String),
}
