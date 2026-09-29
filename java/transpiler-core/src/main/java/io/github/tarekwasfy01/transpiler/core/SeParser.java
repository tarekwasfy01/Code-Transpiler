package io.github.tarekwasfy01.transpiler.core;
import java.io.*; import java.nio.file.*; import java.util.*; import java.util.regex.*;
public final class SeParser {
  private static final Pattern NODE=Pattern.compile("\\{\\"id\\":(\\d+),\\"structural_kind\\":\\"([^\\"]+)\\",\\"fields\\":\\{(.*)}}");
  private static final Pattern KV=Pattern.compile("\\"(kind|name|scope_id)\\"\\s*:\\s*(?:\\"([^\\"]*)\\"|(\\d+))");
  private SeParser() {}
  public static SemanticProgram parse(Path path) throws IOException {
    String projection="semantic_document.v1", evaluation="eager_left_to_right"; int indexBase=0; List<SemanticNode> nodes=new ArrayList<>();
    try (BufferedReader br=Files.newBufferedReader(path)) { String line; while((line=br.readLine())!=null){ line=line.trim();
      if(line.startsWith("projection=")) projection=unquote(line.substring(11));
      else if(line.startsWith("evaluation=")) evaluation=unquote(line.substring(11));
      else if(line.startsWith("index_base=")) indexBase=Integer.parseInt(line.substring(11).trim());
      else { if(line.startsWith(",")) line=line.substring(1); Matcher m=NODE.matcher(line); if(m.matches()){
        Map<String,String> fields=new LinkedHashMap<>(); Matcher kv=KV.matcher(m.group(3)); while(kv.find()) fields.put(kv.group(1), kv.group(2)!=null?kv.group(2):kv.group(3));
        nodes.add(new SemanticNode(Long.parseLong(m.group(1)),m.group(2),fields));
      }}
    }}
    return new SemanticProgram(projection,evaluation,indexBase,nodes);
  }
  private static String unquote(String s){ s=s.trim(); return s.length()>1&&s.charAt(0)=='"'&&s.charAt(s.length()-1)=='"'?s.substring(1,s.length()-1):s; }
}
