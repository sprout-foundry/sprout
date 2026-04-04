/**
 * snippets.ts — Tab-triggered snippet expansion for CodeMirror 6.
 *
 * Provides language-specific code snippets that expand when the user types
 * a trigger word (e.g. `for`, `ifn`, `fn`) and presses Tab.  Once expanded,
 * Tab / Shift+Tab navigate between placeholder fields (handled by the
 * built-in snippet keymap in @codemirror/autocomplete at Prec.highest).
 *
 * Usage in EditorPane:
 * ```ts
 * import { tabExpandSnippets, setSnippetLanguage } from '../extensions/snippets';
 *
 * // In the extensions array (near keymap bindings):
 * extensions: [..., tabExpandSnippets(), ...]
 *
 * // When language changes:
 * setSnippetLanguage(view, languageId);
 * ```
 */

import { keymap, EditorView } from '@codemirror/view';
import { type Extension, Facet, Compartment } from '@codemirror/state';
import { snippet, hasNextSnippetField } from '@codemirror/autocomplete';
import { debugLog } from '../utils/log';

// ── Snippet definitions ─────────────────────────────────────────────

/**
 * Go snippets (language ID: 'go').
 *
 * Template syntax:
 * - `${1:name}` → numbered placeholder with default text
 * - `${0}`      → final exit position
 * - `\n` / `\t` → newline / tab characters
 */
const GO_SNIPPETS: [string, string][] = [
  ['for', 'for ${1:i} := 0; ${1:i} < ${2:n}; ${1:i}++ {\n\t${0}\n}'],
  ['forr', 'for ${1:key}, ${2:value} := range ${3:collection} {\n\t${0}\n}'],
  ['fn', 'func ${1:name}(${2:params}) ${3:returnType} {\n\t${0}\n}'],
  ['fnm', 'func main() {\n\t${0}\n}'],
  ['if', 'if ${1:condition} {\n\t${0}\n}'],
  ['ifn', 'if ${1:condition} {\n\t${2}\n} else {\n\t${3}\n}'],
  ['ife', 'if ${1:err} := ${2:call}; ${1:err} != nil {\n\t${0}\n}'],
  ['sw', 'switch ${1:expr} {\n\tcase ${2:value}:\n\t\t${0}\n}'],
  ['sel', 'select {\n\tcase ${1:msg} := <-${2:ch}:\n\t\t${0}\n}'],
  ['tp', 'type ${1:Name} struct {\n\t${0}\n}'],
  ['itf', 'type ${1:Name} interface {\n\t${0}\n}'],
  ['mt', 'func (${1:recv} *${2:Type}) ${3:Method}(${4:params}) ${5:return} {\n\t${0}\n}'],
  ['go', 'go func() {\n\t${0}\n}()'],
  ['err', 'if err != nil {\n\t${0}\n}'],
  ['vr', 'var ${1:name} ${2:type} = ${0}'],
  ['cn', 'const ${1:name} ${2:type} = ${0}'],
  ['ret', 'return ${0}'],
  ['println', 'fmt.Println(${0})'],
  ['printf', 'fmt.Printf("${1:%s}\\n", ${0})'],
];

/**
 * TypeScript snippets (language IDs: 'typescript', 'typescript-jsx').
 */
const TYPESCRIPT_SNIPPETS: [string, string][] = [
  ['fn', 'function ${1:name}(${2:params}) {\n\t${0}\n}'],
  ['afn', 'const ${1:name} = (${2:params}) => {\n\t${0}\n};'],
  ['if', 'if (${1:condition}) {\n\t${0}\n}'],
  ['ifn', 'if (${1:condition}) {\n\t${2}\n} else {\n\t${3}\n}'],
  ['ife', 'if (${1:condition}) {\n\t${2}\n} else if (${3:condition}) {\n\t${4}\n}'],
  ['for', 'for (let ${1:i} = 0; ${1:i} < ${2:n}; ${1:i}++) {\n\t${0}\n}'],
  ['forof', 'for (const ${1:item} of ${2:iterable}) {\n\t${0}\n}'],
  ['forin', 'for (const ${1:key} in ${2:object}) {\n\t${0}\n}'],
  ['log', 'console.log(${0});'],
  ['try', 'try {\n\t${1}\n} catch (${2:error}) {\n\t${0}\n}'],
  ['cl', 'class ${1:Name} {\n\tconstructor(${2:params}) {\n\t}\n\t${0}\n}'],
  ['im', "import ${0} from '';"],
  ['imn', "import { ${0} } from '';"],
  ['int', 'interface ${1:Name} {\n\t${0}\n}'],
  ['ex', 'export ${0}'],
  ['exd', 'export default ${0}'],
  ['async', 'export const ${1:name} = async (${2:params}) => {\n\t${0}\n};'],
  ['tw', 'export type ${1:Name} = ${0};'],
];

/**
 * JavaScript snippets (language IDs: 'javascript', 'javascript-jsx').
 */
const JAVASCRIPT_SNIPPETS: [string, string][] = [
  ['fn', 'function ${1:name}(${2:params}) {\n\t${0}\n}'],
  ['afn', 'const ${1:name} = (${2:params}) => {\n\t${0}\n};'],
  ['if', 'if (${1:condition}) {\n\t${0}\n}'],
  ['ifn', 'if (${1:condition}) {\n\t${2}\n} else {\n\t${3}\n}'],
  ['ife', 'if (${1:condition}) {\n\t${2}\n} else if (${3:condition}) {\n\t${4}\n}'],
  ['for', 'for (let ${1:i} = 0; ${1:i} < ${2:n}; ${1:i}++) {\n\t${0}\n}'],
  ['forof', 'for (const ${1:item} of ${2:iterable}) {\n\t${0}\n}'],
  ['forin', 'for (const ${1:key} in ${2:object}) {\n\t${0}\n}'],
  ['log', 'console.log(${0});'],
  ['try', 'try {\n\t${1}\n} catch (${2:error}) {\n\t${0}\n}'],
  ['cl', 'class ${1:Name} {\n\tconstructor(${2:params}) {\n\t}\n\t${0}\n}'],
  ['im', "import ${0} from '';"],
  ['imn', "import { ${0} } from '';"],
  ['ex', 'export ${0}'],
  ['exd', 'export default ${0}'],
  ['async', 'export const ${1:name} = async (${2:params}) => {\n\t${0}\n};'],
];

/**
 * Python snippets (language ID: 'python').
 */
const PYTHON_SNIPPETS: [string, string][] = [
  ['fn', 'def ${1:function_name}(${2:params}):\n\t${0}'],
  ['if', 'if ${1:condition}:\n\t${0}'],
  ['ifn', 'if ${1:condition}:\n\t${2}\nelse:\n\t${0}'],
  ['ife', 'if ${1:condition}:\n\t${2}\nelif ${3:condition}:\n\t${0}'],
  ['for', 'for ${1:item} in ${2:iterable}:\n\t${0}'],
  ['wh', 'while ${1:condition}:\n\t${0}'],
  ['class', 'class ${1:ClassName}:\n\t${0}'],
  ['main', "if __name__ == '__main__':\n\t${0}"],
  ['try', 'try:\n\t${1}\nexcept ${2:Exception} as ${3:e}:\n\t${0}'],
  ['with', 'with ${1:expr} as ${2:var}:\n\t${0}'],
  ['pr', 'print(${0})'],
  ['imp', 'import ${0}'],
  ['fr', 'from ${1:module} import ${0}'],
  ['lc', '[${1:expr} for ${2:item} in ${3:iterable} if ${4:condition}]${0}'],
  ['dc', '{${1:key}: ${2:value} for ${3:key}, ${4:value} in ${5:iterable}}${0}'],
  ['lam', 'lambda ${1:params}: ${0}'],
  ['init', 'def __init__(self${2:, params}):\n\t${0}'],
];

/**
 * Rust snippets (language ID: 'rust').
 */
const RUST_SNIPPETS: [string, string][] = [
  ['fn', 'fn ${1:function_name}(${2:params}) {\n\t${0}\n}'],
  ['if', 'if ${1:condition} {\n\t${0}\n}'],
  ['ifn', 'if ${1:condition} {\n\t${2}\n} else {\n\t${3}\n}'],
  ['for', 'for ${1:item} in ${2:iter} {\n\t${0}\n}'],
  ['impl', 'impl ${1:StructName} {\n\t${0}\n}'],
  ['st', 'struct ${1:Name} {\n\t${0}\n}'],
  ['en', 'enum ${1:Name} {\n\t${0}\n}'],
  ['match', 'match ${1:expr} {\n\t${2:_} => ${0}\n}'],
  ['md', 'mod ${1:name};${0}'],
  ['tt', 'trait ${1:Name} {\n\t${0}\n}'],
  ['mac', 'macro_rules! ${1:name} {\n\t(${2:matcher}) => {\n\t\t${0}\n\t};\n}'],
];

/**
 * Java snippets (language ID: 'java').
 */
const JAVA_SNIPPETS: [string, string][] = [
  ['for', 'for (int ${1:i} = 0; ${1:i} < ${2:n}; ${1:i}++) {\n\t${0}\n}'],
  ['fori', 'for (${1:type} ${2:var} : ${3:collection}) {\n\t${0}\n}'],
  ['if', 'if (${1:condition}) {\n\t${0}\n}'],
  ['ifn', 'if (${1:condition}) {\n\t${2}\n} else {\n\t${3}\n}'],
  ['class', 'public class ${1:ClassName} {\n\t${0}\n}'],
  ['meth', 'public ${1:void} ${2:methodName}(${3:params}) {\n\t${0}\n}'],
  ['main', 'public static void main(String[] args) {\n\t${0}\n}'],
  ['sysout', 'System.out.println(${0});'],
  ['syso', 'System.out.println(${0});'],
  ['try', 'try {\n\t${1}\n} catch (${2:Exception} ${3:e}) {\n\t${4}\n} finally {\n\t${5}\n}'],
  ['st', 'private ${1:String} ${2:name};${0}'],
];

/**
 * C snippets (language ID: 'c').
 */
const C_SNIPPETS: [string, string][] = [
  ['for', 'for (int ${1:i} = 0; ${1:i} < ${2:n}; ${1:i}++) {\n\t${0}\n}'],
  ['if', 'if (${1:condition}) {\n\t${0}\n}'],
  ['ifn', 'if (${1:condition}) {\n\t${2}\n} else {\n\t${3}\n}'],
  ['inc', '#include <${0}>'],
  ['main', 'int main(int argc, char* argv[]) {\n\t${0}\n\treturn 0;\n}'],
  ['while', 'while (${1:condition}) {\n\t${0}\n}'],
  ['do', 'do {\n\t${0}\n} while (${1:condition});'],
];

/**
 * C++ snippets (language ID: 'cpp').
 */
const CPP_SNIPPETS: [string, string][] = [
  ['for', 'for (int ${1:i} = 0; ${1:i} < ${2:n}; ${1:i}++) {\n\t${0}\n}'],
  ['if', 'if (${1:condition}) {\n\t${0}\n}'],
  ['ifn', 'if (${1:condition}) {\n\t${2}\n} else {\n\t${3}\n}'],
  ['inc', '#include <${0}>'],
  ['main', 'int main(int argc, char* argv[]) {\n\t${0}\n\treturn 0;\n}'],
  ['while', 'while (${1:condition}) {\n\t${0}\n}'],
  ['do', 'do {\n\t${0}\n} while (${1:condition});'],
  ['class', 'class ${1:ClassName} {\npublic:\n\t${0}\n};'],
  ['str', 'std::string ${1:name} = "${0}";'],
  ['vec', 'std::vector<${1:type}> ${2:name};${0}'],
];

/**
 * PHP snippets (language ID: 'php').
 */
const PHP_SNIPPETS: [string, string][] = [
  ['fn', 'function ${1:name}(${2:params}) {\n\t${0}\n}'],
  ['if', 'if (${1:condition}) {\n\t${0}\n}'],
  ['ifn', 'if (${1:condition}) {\n\t${2}\n} else {\n\t${3}\n}'],
  ['class', 'class ${1:ClassName} {\n\t${0}\n}'],
  ['ec', 'echo ${0};'],
  ['pr', 'print_r(${0});'],
];

/**
 * Ruby snippets (language ID: 'ruby').
 */
const RUBY_SNIPPETS: [string, string][] = [
  ['fn', 'def ${1:method_name}(${2:params})\n\t${0}\nend'],
  ['if', 'if ${1:condition}\n\t${0}\nend'],
  ['ifn', 'if ${1:condition}\n\t${2}\nelse\n\t${3}\nend'],
  ['class', 'class ${1:ClassName}\n\t${0}\nend'],
  ['mod', 'module ${1:ModuleName}\n\t${0}\nend'],
  ['each', '${1:collection}.each do |${2:element}|\n\t${0}\nend'],
  ['puts', 'puts ${0}'],
  ['req', "require '${0}'"],
];

/**
 * Shell/Bash snippets (language ID: 'shell').
 */
const SHELL_SNIPPETS: [string, string][] = [
  ['if', 'if [[ ${1:condition} ]]; then\n\t${0}\nfi'],
  ['ifn', 'if [[ ${1:condition} ]]; then\n\t${2}\nelse\n\t${3}\nfi'],
  ['ife', 'if [[ ${1:condition} ]]; then\n\t${2}\nelif [[ ${3:condition} ]]; then\n\t${4}\nelse\n\t${5}\nfi'],
  ['for', 'for ${1:var} in ${2:list}; do\n\t${0}\ndone'],
  ['wh', 'while [[ ${1:condition} ]]; do\n\t${0}\ndone'],
  ['case', 'case ${1:value} in\n\t${2:pattern})\n\t\t${0}\n\t\t;;\nesac'],
  ['func', '${1:name}() {\n\t${0}\n}'],
  ['shebang', '#!/bin/bash\n\n${0}'],
  ['echo', 'echo "${0}"'],
];

/**
 * HTML snippets (language ID: 'html').
 */
const HTML_SNIPPETS: [string, string][] = [
  ['div', '<div>\n\t${0}\n</div>'],
  ['span', '<span>${0}</span>'],
  ['p', '<p>\n\t${0}\n</p>'],
  ['a', '<a href="${1:#}">${0}</a>'],
  ['img', '<img src="${1}" alt="${0}" />'],
  ['ul', '<ul>\n\t<li>${0}</li>\n</ul>'],
  ['ol', '<ol>\n\t<li>${0}</li>\n</ol>'],
  ['li', '<li>${0}</li>'],
  ['h1', '<h1>${0}</h1>'],
  ['h2', '<h2>${0}</h2>'],
  ['h3', '<h3>${0}</h3>'],
  ['h4', '<h4>${0}</h4>'],
  ['input', '<input type="${1:text}" id="${2}" name="${0}" />'],
  ['btn', '<button type="button"${1}>${0}</button>'],
  ['form', '<form action="${1}" method="${2:post}">\n\t${0}\n</form>'],
  ['meta', '<meta charset="${0}" />'],
  ['link', '<link rel="${1:stylesheet}" href="${0}" />'],
  ['script', '<script>\n\t${0}\n</script>'],
  ['style', '<style>\n\t${0}\n</style>'],
  [
    'html',
    '<!DOCTYPE html>\n<html lang="${1:en}">\n<head>\n\t<meta charset="UTF-8" />\n\t<meta name="viewport" content="width=device-width, initial-scale=1.0" />\n\t<title>${2}</title>\n</head>\n<body>\n\t${0}\n</body>\n</html>',
  ],
];

/**
 * SQL snippets (language ID: 'sql').
 */
const SQL_SNIPPETS: [string, string][] = [
  ['sel', 'SELECT ${1:*} FROM ${0}'],
  ['selw', 'SELECT ${1:*} FROM ${2:table} WHERE ${0}'],
  ['ins', 'INSERT INTO ${1:table} (${2:columns}) VALUES (${0});'],
  ['upd', 'UPDATE ${1:table} SET ${2:column} = ${3:value} WHERE ${0};'],
  ['del', 'DELETE FROM ${1:table} WHERE ${0};'],
  ['ct', 'CREATE TABLE ${1:table_name} (\n\t${0}\n);'],
  ['join', 'INNER JOIN ${1:table} ON ${0}'],
  ['lj', 'LEFT JOIN ${1:table} ON ${0}'],
];

/**
 * YAML snippets (language ID: 'yaml').
 */
const YAML_SNIPPETS: [string, string][] = [
  ['key', '${1:key}: ${0}'],
  ['list', '${1}:\n\t- ${0}'],
];

/**
 * Markdown snippets (language ID: 'markdown').
 */
const MARKDOWN_SNIPPETS: [string, string][] = [
  ['code', '```\n${0}\n```'],
  ['link', '[${1:text}](${2:url})${0}'],
  ['img', '![${1:alt}](${2:url})${0}'],
  ['bold', '**${0}**'],
  ['italic', '*${0}*'],
  ['h1', '# ${0}'],
  ['h2', '## ${0}'],
  ['h3', '### ${0}'],
  ['h4', '#### ${0}'],
  ['ul', '- ${0}'],
  ['ol', '1. ${0}'],
  ['task', '- [ ] ${0}'],
  ['tbl', '| ${1:Header 1} | ${2:Header 2} | ${3:Header 3} |\n| --- | --- | --- |\n| ${4} | ${5} | ${6} |${0}'],
  ['quote', '> ${0}'],
  ['hr', '---${0}'],
];

/**
 * Swift snippets (language ID: 'swift').
 */
const SWIFT_SNIPPETS: [string, string][] = [
  ['if', 'if ${1:condition} {\n\t${0}\n}'],
  ['ifn', 'if ${1:condition} {\n\t${2}\n} else {\n\t${3}\n}'],
  ['ife', 'if ${1:condition} {\n\t${2}\n} else if ${3:condition} {\n\t${4}\n} else {\n\t${5}\n}'],
  ['for', 'for ${1:item} in ${2:collection} {\n\t${0}\n}'],
  ['wh', 'while ${1:condition} {\n\t${0}\n}'],
  ['fn', 'func ${1:name}(${2:params}) {\n\t${0}\n}'],
  ['guard', 'guard ${1:condition} else {\n\t${0}\n\treturn\n}'],
  ['cls', 'class ${1:ClassName} {\n\t${0}\n}'],
  ['st', 'struct ${1:StructName} {\n\t${0}\n}'],
  ['prt', 'print(${0})'],
  ['sw', 'switch ${1:value} {\ncase ${2:pattern}:\n\t${0}\ndefault:\n\tbreak\n}'],
  ['do', 'do {\n\ttry ${0}\n} catch {\n\tprint(error)\n}'],
];

/**
 * Kotlin snippets (language ID: 'kotlin').
 */
const KOTLIN_SNIPPETS: [string, string][] = [
  ['if', 'if (${1:condition}) {\n\t${0}\n}'],
  ['ifn', 'if (${1:condition}) {\n\t${2}\n} else {\n\t${3}\n}'],
  ['for', 'for (${1:item} in ${2:collection}) {\n\t${0}\n}'],
  ['wh', 'while (${1:condition}) {\n\t${0}\n}'],
  ['fn', 'fun ${1:name}(${2:params}): ${3:ReturnType} {\n\t${0}\n}'],
  ['cls', 'class ${1:ClassName} {\n\t${0}\n}'],
  ['obj', 'object ${1:ObjectName} {\n\t${0}\n}'],
  ['when', 'when (${1:value}) {\n\t${2:pattern} -> ${0}\n\telse -> {}\n}'],
  ['println', 'println(${0})'],
  ['val', 'val ${1:name} = ${0}'],
  ['var', 'var ${1:name} = ${0}'],
];

/**
 * Dart snippets (language ID: 'dart').
 */
const DART_SNIPPETS: [string, string][] = [
  ['fn', '${1:ReturnType} ${2:name}(${3:params}) {\n\t${0}\n}'],
  ['cls', 'class ${1:ClassName} {\n\t${0}\n}'],
  ['if', 'if (${1:condition}) {\n\t${0}\n}'],
  ['ifn', 'if (${1:condition}) {\n\t${2}\n} else {\n\t${3}\n}'],
  ['for', 'for (var ${1:item} in ${2:collection}) {\n\t${0}\n}'],
  ['wh', 'while (${1:condition}) {\n\t${0}\n}'],
  ['main', 'void main() {\n\t${0}\n}'],
];

/**
 * Scala snippets (language ID: 'scala').
 */
const SCALA_SNIPPETS: [string, string][] = [
  ['fn', 'def ${1:name}(${2:params}): ${3:ReturnType} = {\n\t${0}\n}'],
  ['cls', 'class ${1:ClassName} {\n\t${0}\n}'],
  ['obj', 'object ${1:ObjectName} {\n\t${0}\n}'],
  ['trt', 'trait ${1:TraitName} {\n\t${0}\n}'],
  ['if', 'if (${1:condition}) {\n\t${0}\n}'],
  ['ifn', 'if (${1:condition}) {\n\t${2}\n} else {\n\t${3}\n}'],
  ['for', 'for (${1:item} <- ${2:collection}) {\n\t${0}\n}'],
  ['match', '${1:value} match {\n\tcase ${2:pattern} => ${0}\n\tcase _ =>\n}'],
  ['println', 'println(${0})'],
];

/**
 * C# snippets (language ID: 'csharp').
 */
const CSHARP_SNIPPETS: [string, string][] = [
  ['if', 'if (${1:condition}) {\n\t${0}\n}'],
  ['ifn', 'if (${1:condition}) {\n\t${2}\n} else {\n\t${3}\n}'],
  ['for', 'for (int ${1:i} = 0; ${1:i} < ${2:n}; ${1:i}++) {\n\t${0}\n}'],
  ['fore', 'foreach (var ${1:item} in ${2:collection}) {\n\t${0}\n}'],
  ['wh', 'while (${1:condition}) {\n\t${0}\n}'],
  ['cw', 'Console.WriteLine(${0});'],
  ['cls', 'public class ${1:ClassName} {\n\t${0}\n}'],
  ['meth', 'public ${1:void} ${2:MethodName}(${3:params}) {\n\t${0}\n}'],
  ['prop', 'public ${1:type} ${2:Name} { get; set; }${0}'],
  ['try', 'try {\n\t${1}\n} catch (${2:Exception} ${3:e}) {\n\t${4}\n} finally {\n\t${5}\n}'],
];

/**
 * Lua snippets (language ID: 'lua').
 */
const LUA_SNIPPETS: [string, string][] = [
  ['fn', 'local function ${1:name}(${2:params})\n\t${0}\nend'],
  ['if', 'if ${1:condition} then\n\t${0}\nend'],
  ['ifn', 'if ${1:condition} then\n\t${2}\nelse\n\t${3}\nend'],
  ['for', 'for ${1:i} = ${2:1}, ${3:n} do\n\t${0}\nend'],
  ['wh', 'while ${1:condition} do\n\t${0}\nend'],
  ['pr', 'print(${0})'],
];

/**
 * Groovy snippets (language ID: 'groovy').
 */
const GROOVY_SNIPPETS: [string, string][] = [
  ['cls', 'class ${1:ClassName} {\n\t${0}\n}'],
  ['fn', 'def ${1:name}(${2:params}) {\n\t${0}\n}'],
  ['if', 'if (${1:condition}) {\n\t${0}\n}'],
  ['ifn', 'if (${1:condition}) {\n\t${2}\n} else {\n\t${3}\n}'],
  ['for', 'for (${1:item} in ${2:collection}) {\n\t${0}\n}'],
  ['wh', 'while (${1:condition}) {\n\t${0}\n}'],
  ['println', 'println ${0}'],
  ['each', '${1:collection}.each { ${0} ->\n\t\n}'],
];

// ── Language → snippet mapping ──────────────────────────────────────

/**
 * Maps language IDs to their snippet definitions.
 * Multiple language IDs can share the same snippet array.
 */
const LANGUAGE_SNIPPETS = new Map<string, [string, string][]>([
  ['go', GO_SNIPPETS],
  ['typescript', TYPESCRIPT_SNIPPETS],
  ['typescript-jsx', TYPESCRIPT_SNIPPETS],
  ['javascript', JAVASCRIPT_SNIPPETS],
  ['javascript-jsx', JAVASCRIPT_SNIPPETS],
  ['python', PYTHON_SNIPPETS],
  ['rust', RUST_SNIPPETS],
  ['java', JAVA_SNIPPETS],
  ['c', C_SNIPPETS],
  ['cpp', CPP_SNIPPETS],
  ['php', PHP_SNIPPETS],
  ['ruby', RUBY_SNIPPETS],
  ['shell', SHELL_SNIPPETS],
  ['html', HTML_SNIPPETS],
  ['sql', SQL_SNIPPETS],
  ['yaml', YAML_SNIPPETS],
  ['markdown', MARKDOWN_SNIPPETS],
  ['swift', SWIFT_SNIPPETS],
  ['kotlin', KOTLIN_SNIPPETS],
  ['dart', DART_SNIPPETS],
  ['scala', SCALA_SNIPPETS],
  ['csharp', CSHARP_SNIPPETS],
  ['lua', LUA_SNIPPETS],
  ['groovy', GROOVY_SNIPPETS],
]);

// Pre-built Maps for each language (computed once, reused).
const snippetCache = new Map<string, Map<string, string>>();

// ── Per-view language state ────────────────────────────────────────
//
// Uses a Facet so each EditorView instance stores its own language ID.
// The Facet lives inside a Compartment so the language can be changed
// at runtime (when the user switches buffers in a pane) via
// `setSnippetLanguage(view, langId)`.

const snippetLanguageFacet = Facet.define<string | null, string | null>({
  combine: (values) => values[values.length - 1] ?? null,
});

/** Compartment for swapping the snippet language per-editor-instance. */
const langCompartment = new Compartment();

// For backward compat / testing: track the most recently set language
// so `getSnippetLanguage()` works in non-EditorView contexts.
let _lastSetLanguageId: string | null = null;

// ── Public API ──────────────────────────────────────────────────────

/**
 * Update the snippet language for a specific EditorView instance.
 *
 * Called by the editor host (EditorPane) whenever the buffer language
 * changes.  A single module-level Compartment is shared across views
 * (the standard CodeMirror pattern), but `view.dispatch()` targets only
 * the specific pane, so two panes showing files in different languages
 * won't interfere with each other.
 */
export function setSnippetLanguage(view: EditorView, langId: string | null): void {
  _lastSetLanguageId = langId;
  try {
    view.dispatch({
      effects: langCompartment.reconfigure(snippetLanguageFacet.of(langId)),
    });
  } catch (err) {
    debugLog('[setSnippetLanguage] failed to dispatch snippet language compartment:', err);
    // Edge case: view already destroyed or compartment not attached.
    // Ignore silently — the next Tab press will see the fresh language
    // when the editor is re-initialised for a different buffer.
  }
}

/**
 * Get the last-set language ID.
 * Exposed for testing only.
 */
export function getSnippetLanguage(): string | null {
  return _lastSetLanguageId;
}

/**
 * Return a Map of trigger-word → template for the given language.
 *
 * If the language has no registered snippets, an empty Map is returned.
 * Results are cached per-language for fast repeated lookups.
 *
 * The returned Map uses lowercase trigger words as keys.
 */
export function getSnippetsForLanguage(langId: string | null): Map<string, string> {
  if (!langId) return new Map();

  const cached = snippetCache.get(langId);
  if (cached) return cached;

  const defs = LANGUAGE_SNIPPETS.get(langId);
  if (!defs) return new Map();

  const m = new Map<string, string>(defs.map(([trigger, template]) => [trigger, template]));
  snippetCache.set(langId, m);
  return m;
}

// ── Word-before-cursor helper ───────────────────────────────────────

/**
 * Extract the alphanumeric+underscore word immediately before `pos` in
 * the document.  Returns `{ word, from }` where `word` is the matched
 * text (already lowercased) and `from` is its document position, or
 * `null` if there is no word terminating at `pos`.
 */
function wordBeforeCursor(
  doc: {
    sliceString(from: number, to: number): string;
    lineAt(pos: number): { text: string; from: number; number: number };
  },
  pos: number,
): { word: string; from: number } | null {
  if (pos <= 0) return null;

  const line = doc.lineAt(pos);
  const textBefore = line.text.slice(0, pos - line.from);
  const match = textBefore.match(/[a-zA-Z_][a-zA-Z0-9_]*$/);
  if (!match) return null;

  const word = match[0];
  return { word: word.toLowerCase(), from: pos - word.length };
}

// ── Theme ───────────────────────────────────────────────────────────

/**
 * Base theme styles for snippet field highlighting.
 *
 * CodeMirror's autocomplete extension provides base styling for
 * `.cm-snippetField` / `.cm-snippetFieldActive`, but we layer our own
 * theme on top for a more visible, themed appearance.
 */
const snippetTheme = EditorView.baseTheme({
  '.cm-snippetField': {
    backgroundColor: 'rgba(99, 102, 241, 0.15)',
    outline: '1px solid rgba(99, 102, 241, 0.3)',
    borderRadius: '2px',
    color: 'inherit',
  },
  '.cm-snippetFieldActive': {
    backgroundColor: 'rgba(99, 102, 241, 0.3) !important',
    outline: '1px solid rgba(99, 102, 241, 0.6) !important',
  },
});

// ── Tab expansion keymap ────────────────────────────────────────────

/**
 * Returns a CodeMirror Extension that binds Tab to snippet expansion.
 *
 * When the user presses Tab:
 * 1. If a snippet is already active (has next field), the built-in
 *    snippet keymap (at Prec.highest) handles navigation — we do nothing.
 * 2. Otherwise, we check whether the word before the cursor matches a
 *    snippet trigger for the current language.
 * 3. If it matches, we replace the trigger word with the expanded snippet.
 * 4. If no match, we return `false` so the next keymap handler
 *    (e.g. `indentWithTab`) can handle the Tab press normally.
 *
 * Include near the other keymap extensions in the editor setup:
 * ```ts
 * extensions: [..., tabExpandSnippets(), keymap.of([indentWithTab]), ...]
 * ```
 */
export function tabExpandSnippets(): Extension {
  return [
    snippetTheme,
    langCompartment.of(snippetLanguageFacet.of(null)),
    keymap.of([
      {
        key: 'Tab',
        run(view: EditorView): boolean {
          // If a snippet session is already active, the Prec.highest
          // snippet keymap will have already consumed the key.  As a
          // safety net, skip our expansion logic when fields remain.
          if (hasNextSnippetField(view.state)) return false;

          const sel = view.state.selection.main;
          // Only expand when the cursor is a simple caret (no selection).
          if (sel.from !== sel.to) return false;

          const wordInfo = wordBeforeCursor(view.state.doc, sel.from);
          if (!wordInfo) return false;

          const langId = view.state.facet(snippetLanguageFacet);
          const snippets = getSnippetsForLanguage(langId);
          const template = snippets.get(wordInfo.word);
          if (!template) return false;

          // Apply the snippet: replaces the trigger word with the
          // expanded template and activates tab-stop navigation.
          const applySnippet = snippet(template);
          applySnippet(view, null, wordInfo.from, sel.from);
          return true;
        },
      },
    ]),
  ];
}
