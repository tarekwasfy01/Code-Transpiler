package io.github.tarekwasfy01.transpiler.core;
import java.util.*;
/** Native Java representation of the pure-Rust port's language-neutral IR. */
public final class NativeIr {
 private NativeIr(){}
 public record Program(List<StructDef> structs,List<Function> functions){public Program{structs=List.copyOf(structs);functions=List.copyOf(functions);}}
 public record StructDef(String name,List<StructField> fields){public StructDef{fields=List.copyOf(fields);}}
 public record StructField(String name,Type type){}
 public record Function(String name,List<Param> params,Optional<Type> returnType,List<Stmt> body){public Function{params=List.copyOf(params);returnType=returnType==null?Optional.empty():returnType;body=List.copyOf(body);}}
 public record Param(String name,Type type){}
 public sealed interface Type permits UnitType,ScalarType,SliceType,MapType,TupleType,NamedType {}
 public record UnitType() implements Type{}
 public enum ScalarType implements Type { BOOL,STRING,INT,INT8,INT16,INT32,INT64,UINT,UINT8,UINT16,UINT32,UINT64,FLOAT32,FLOAT64 }
 public record SliceType(Type element) implements Type{}
 public record MapType(Type key,Type value) implements Type{}
 public record TupleType(List<Type> elements) implements Type{public TupleType{elements=List.copyOf(elements);}}
 public record NamedType(String name) implements Type{}
 public sealed interface Stmt permits Let,MultiLet,Assign,IndexAssign,MultiAssign,ExprStmt,Print,Return,If,While,ForEach,Switch,Break,Continue {}
 public record Let(String name,Optional<Type> type,Expr value,boolean mutable) implements Stmt{public Let{type=type==null?Optional.empty():type;}}
 public record MultiLet(List<String> names,Expr value,boolean mutable) implements Stmt{public MultiLet{names=List.copyOf(names);}}
 public record Assign(String target,String op,Expr value) implements Stmt{}
 public record IndexAssign(Expr collection,Expr index,String op,Expr value,Optional<Type> mapValueType) implements Stmt{public IndexAssign{mapValueType=mapValueType==null?Optional.empty():mapValueType;}}
 public record MultiAssign(List<String> targets,Expr value) implements Stmt{public MultiAssign{targets=List.copyOf(targets);}}
 public record ExprStmt(Expr expr) implements Stmt{}
 public record Print(boolean newline,List<Expr> args) implements Stmt{public Print{args=List.copyOf(args);}}
 public record Return(Optional<Expr> value) implements Stmt{public Return{value=value==null?Optional.empty():value;}}
 public record If(Expr cond,List<Stmt> thenBody,List<Stmt> elseBody) implements Stmt{public If{thenBody=List.copyOf(thenBody);elseBody=List.copyOf(elseBody);}}
 public record While(Expr cond,List<Stmt> body) implements Stmt{public While{body=List.copyOf(body);}}
 public record ForEach(String value,Expr iter,List<Stmt> body) implements Stmt{public ForEach{body=List.copyOf(body);}}
 public record Switch(Optional<Expr> value,List<SwitchCase> cases,List<Stmt> defaultBody) implements Stmt{public Switch{value=value==null?Optional.empty():value;cases=List.copyOf(cases);defaultBody=List.copyOf(defaultBody);}}
 public record Break() implements Stmt{} public record Continue() implements Stmt{}
 public record SwitchCase(List<Expr> values,List<Stmt> body){public SwitchCase{values=List.copyOf(values);body=List.copyOf(body);}}
 public sealed interface Expr permits Ident,IntLiteral,FloatLiteral,StringLiteral,BoolLiteral,Nil,Unary,Binary,Call,Index,Array,MapExpr,MapIndex,MapLookupOk,TupleExpr,StructLiteral,Raw {}
 public record Ident(String name) implements Expr{} public record IntLiteral(String text) implements Expr{} public record FloatLiteral(String text) implements Expr{} public record StringLiteral(String text) implements Expr{} public record BoolLiteral(boolean value) implements Expr{} public record Nil() implements Expr{}
 public record Unary(String op,Expr value) implements Expr{} public record Binary(Expr left,String op,Expr right) implements Expr{} public record Call(String function,List<Expr> args) implements Expr{public Call{args=List.copyOf(args);}}
 public record Index(Expr value,Expr index) implements Expr{} public record Array(List<Expr> items,Optional<Type> elementType) implements Expr{public Array{items=List.copyOf(items);elementType=elementType==null?Optional.empty():elementType;}}
 public record MapEntry(Expr key,Expr value){} public record MapExpr(Type keyType,Type valueType,List<MapEntry> entries) implements Expr{public MapExpr{entries=List.copyOf(entries);}}
 public record MapIndex(Expr value,Expr index,Type valueType) implements Expr{} public record MapLookupOk(Expr value,Expr index,Type valueType) implements Expr{}
 public record TupleExpr(List<Expr> values) implements Expr{public TupleExpr{values=List.copyOf(values);}}
 public record FieldValue(String name,Expr value){} public record StructLiteral(String type,List<FieldValue> fields,List<Expr> positional) implements Expr{public StructLiteral{fields=List.copyOf(fields);positional=List.copyOf(positional);}}
 public record Raw(String code) implements Expr{}
}
