import fs from 'node:fs/promises';
import path from 'node:path';
import {createHash} from 'node:crypto';

// Paths and bytes both contribute: renames and changes outside the old six-file
// shortlist (notably matrixir) must invalidate the audit identity.
export async function sourceIdentity(root) {
 const files=[];
 async function walk(relative) {
  for(const entry of await fs.readdir(path.join(root,relative),{withFileTypes:true})) {
   const p=path.posix.join(relative,entry.name);
   if(entry.isDirectory())await walk(p);
   else if(/\.(go|mjs|json)$/.test(p))files.push(p);
  }
 }
 await walk('internal');await walk('cmd');await walk('tools/matrix-audit');await walk('tools/language-feature-matrix');
 files.push('go.mod','go.sum','build-onefile.ps1','assets/code-transpiler.ico');files.sort();
 const source_manifest=await Promise.all(files.map(async p=>({path:p,sha256:createHash('sha256').update(await fs.readFile(path.join(root,p))).digest('hex')})));
 return {source_tree_hash:createHash('sha256').update(JSON.stringify(source_manifest)).digest('hex'),source_manifest};
}
