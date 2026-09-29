package io.github.tarekwasfy01.transpiler.core;
import java.util.*;
public final class BootstrapChunker {
  private BootstrapChunker() {}
  public static List<String> balancedBlocks(String source, int softLimit) {
    List<String> out=new ArrayList<>(); int start=0, depth=0; boolean string=false; char quote=0;
    for(int i=0;i<source.length();i++){ char c=source.charAt(i);
      if(string){ if(c==quote && (i==0||source.charAt(i-1)!='\\')) string=false; continue; }
      if(c=='\''||c=='"'||c=='`'){ string=true; quote=c; continue; }
      if(c=='{'||c=='('||c=='[') depth++; else if(c=='}'||c==')'||c==']') depth=Math.max(0,depth-1);
      if(depth==0 && i-start>=softLimit && (c=='}'||c==';'||c=='\n')){ out.add(source.substring(start,i+1)); start=i+1; }
    }
    if(start<source.length()) out.add(source.substring(start)); return out;
  }
}
