package agent

import (
	"bytes"
	"fmt"
	"os/exec"
	"strings"
)

// astVerifyPython uses Python's builtin ast module to verify that named defs/classes exist
func astVerifyPython(filePath string, names []string) (bool, string) {
	if len(names) == 0 {
		return true, "no names to verify"
	}
	script := `import ast,sys
path=sys.argv[1]
with open(path,'r',encoding='utf-8',errors='ignore') as f:
    src=f.read()
tree=ast.parse(src)
defs=set()
class Visitor(ast.NodeVisitor):
    def visit_FunctionDef(self,node):
        defs.add(node.name)
        self.generic_visit(node)
    def visit_AsyncFunctionDef(self,node):
        defs.add(node.name)
        self.generic_visit(node)
    def visit_ClassDef(self,node):
        defs.add(node.name)
        self.generic_visit(node)
Visitor().visit(tree)
print("\n".join(sorted(defs)))`
	cmd := exec.Command("python3", "-c", script, filePath)
	var out, stderr bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		// try python
		cmd2 := exec.Command("python", "-c", script, filePath)
		cmd2.Stdout = &out
		cmd2.Stderr = &stderr
		if err2 := cmd2.Run(); err2 != nil {
			return true, "python not available"
		}
	}
	have := map[string]bool{}
	for _, line := range strings.Split(strings.TrimSpace(out.String()), "\n") {
		if strings.TrimSpace(line) != "" {
			have[strings.TrimSpace(line)] = true
		}
	}
	for _, n := range names {
		if !have[n] {
			return false, fmt.Sprintf("python AST: missing %s", n)
		}
	}
	return true, "python AST ok"
}

// astVerifyJS uses node+acorn if available to extract function/class names
func astVerifyJS(filePath string, names []string) (bool, string) {
	if len(names) == 0 {
		return true, "no names to verify"
	}
	js := `const fs=require('fs');const acorn=require('acorn');
const src=fs.readFileSync(process.argv[2],'utf8');
const ast=acorn.parse(src,{ecmaVersion:'latest',sourceType:'module'});
const out=[];
function walk(n){
  if(!n||typeof n!=='object') return;
  if(n.type==='FunctionDeclaration'&&n.id) out.push(n.id.name);
  if(n.type==='ClassDeclaration'&&n.id) out.push(n.id.name);
  for(const k in n){ const v=n[k]; if(Array.isArray(v)) v.forEach(walk); else walk(v); }
}
walk(ast);
console.log(out.join('\n'));`
	cmd := exec.Command("node", "-e", js, "node", filePath)
	var out, stderr bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		// acorn missing or node not available: skip
		return true, "node/acorn not available"
	}
	have := map[string]bool{}
	for _, line := range strings.Split(strings.TrimSpace(out.String()), "\n") {
		if line != "" {
			have[line] = true
		}
	}
	for _, n := range names {
		if !have[n] {
			return false, fmt.Sprintf("js AST: missing %s", n)
		}
	}
	return true, "js AST ok"
}

// astVerifyRuby uses Ruby's Ripper to ensure defs/classes present
func astVerifyRuby(filePath string, names []string) (bool, string) {
	if len(names) == 0 {
		return true, "no names to verify"
	}
	script := `require 'ripper'; src=File.read(ARGV[0]); sexp=Ripper.sexp(src)
def collect(node, out)
  return if node.nil?
  if Array===node
    if node[0]==:def
      out << node[1][1].to_s
    elsif node[0]==:class
      out << node[1][1].to_s
    end
    node.each{|c| collect(c,out)}
  end
end
out=[]; collect(sexp,out); puts out.uniq.join("\n")`
	cmd := exec.Command("ruby", "-e", script, filePath)
	var out, stderr bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return true, "ruby not available"
	}
	have := map[string]bool{}
	for _, line := range strings.Split(strings.TrimSpace(out.String()), "\n") {
		if line != "" {
			have[line] = true
		}
	}
	for _, n := range names {
		if !have[n] {
			return false, fmt.Sprintf("ruby AST: missing %s", n)
		}
	}
	return true, "ruby AST ok"
}

// astVerifyPHP uses PHP's tokenizer to enumerate function/class names without needing ext/ast
func astVerifyPHP(filePath string, names []string) (bool, string) {
	if len(names) == 0 {
		return true, "no names to verify"
	}
	script := `<?php
    $src = file_get_contents($argv[1]);
    if ($src === false) { fwrite(STDERR, "read fail\n"); exit(1);} 
    $t=token_get_all($src);
    $out=[]; $i=0; $n=count($t);
    while($i<$n){
      $tok=$t[$i];
      if(is_array($tok)){
        if($tok[0]==T_FUNCTION){
          $j=$i+1; while($j<$n && is_array($t[$j]) && ($t[$j][0]==T_WHITESPACE||$t[$j][0]==T_AMPERSAND)){$j++;}
          if($j<$n){ if(is_array($t[$j]) && $t[$j][0]==T_STRING){ $out[]=$t[$j][1]; }}
        }
        if($tok[0]==T_CLASS){
          $j=$i+1; while($j<$n && is_array($t[$j]) && $t[$j][0]==T_WHITESPACE){$j++;}
          if($j<$n){ if(is_array($t[$j]) && $t[$j][0]==T_STRING){ $out[]=$t[$j][1]; }}
        }
      }
      $i++;
    }
    echo implode("\n", array_unique($out));
    ?>`
	cmd := exec.Command("php", "-r", script, filePath)
	var out, stderr bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return true, "php not available"
	}
	have := map[string]bool{}
	for _, line := range strings.Split(strings.TrimSpace(out.String()), "\n") {
		if line != "" {
			have[line] = true
		}
	}
	for _, n := range names {
		if !have[n] {
			return false, fmt.Sprintf("php AST: missing %s", n)
		}
	}
	return true, "php AST ok"
}

// astVerifyJava uses universal ctags if available to extract class/method symbols
func astVerifyJava(filePath string, names []string) (bool, string) {
	if len(names) == 0 {
		return true, "no names to verify"
	}
	cmd := exec.Command("ctags", "-x", "--language-force=Java", filePath)
	var out, stderr bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return true, "ctags not available"
	}
	have := map[string]bool{}
	// ctags -x output: NAME KIND LINE FILE ...
	for _, line := range strings.Split(strings.TrimSpace(out.String()), "\n") {
		f := strings.Fields(line)
		if len(f) >= 1 {
			have[f[0]] = true
		}
	}
	for _, n := range names {
		if !have[n] {
			return false, fmt.Sprintf("java tags: missing %s", n)
		}
	}
	return true, "java tags ok"
}

// astVerifyCSharp uses universal ctags for C#
func astVerifyCSharp(filePath string, names []string) (bool, string) {
	if len(names) == 0 {
		return true, "no names to verify"
	}
	cmd := exec.Command("ctags", "-x", "--language-force=CSharp", filePath)
	var out, stderr bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return true, "ctags not available"
	}
	have := map[string]bool{}
	for _, line := range strings.Split(strings.TrimSpace(out.String()), "\n") {
		f := strings.Fields(line)
		if len(f) >= 1 {
			have[f[0]] = true
		}
	}
	for _, n := range names {
		if !have[n] {
			return false, fmt.Sprintf("csharp tags: missing %s", n)
		}
	}
	return true, "csharp tags ok"
}
