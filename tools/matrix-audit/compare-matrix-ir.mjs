import fs from 'node:fs/promises';
import path from 'node:path';

const beforeDir = path.resolve('outputs/transpiler-audit-before-matrix-ir');
const afterDir = path.resolve('outputs/transpiler-audit-2026-08-30');
const before = JSON.parse(await fs.readFile(path.join(beforeDir, 'measurements.json'), 'utf8'));
const after = JSON.parse(await fs.readFile(path.join(afterDir, 'measurements.json'), 'utf8'));
const beforeByID = new Map(before.records.map(record => [record.id, record]));
const afterByID = new Map(after.records.map(record => [record.id, record]));
if (beforeByID.size !== afterByID.size) throw new Error('Record count changed');

const stages = ['emit', 'compile', 'run', 'output', 'overall'];
const transitions = Object.fromEntries(stages.map(stage => [stage, {}]));
const changedStatus = [];
const changedCode = [];
for (const [id, oldRecord] of beforeByID) {
  const newRecord = afterByID.get(id);
  if (!newRecord) throw new Error(`Missing route ${id}`);
  for (const stage of stages) {
    const key = `${oldRecord[stage]}>${newRecord[stage]}`;
    transitions[stage][key] = (transitions[stage][key] ?? 0) + 1;
    if (oldRecord[stage] !== newRecord[stage]) changedStatus.push({id, stage, before: oldRecord[stage], after: newRecord[stage]});
  }
  if (oldRecord.target_code !== newRecord.target_code) changedCode.push(id);
}

const result = {
  schema_version: 1,
  before_source_hash: before.summary.source_tree_hash,
  after_source_hash: after.summary.source_tree_hash,
  routes: beforeByID.size,
  status_changes: changedStatus.length,
  generated_code_changes: changedCode.length,
  regressions: changedStatus.filter(change => change.before === 'PASS' && change.after !== 'PASS').length,
  improvements: changedStatus.filter(change => change.before !== 'PASS' && change.after === 'PASS').length,
  transitions,
  changed_status: changedStatus,
  changed_code_ids: changedCode,
};
await fs.writeFile(path.join(afterDir, 'matrix_ir_delta.json'), JSON.stringify(result, null, 2));
console.log(JSON.stringify({...result, changed_status: undefined, changed_code_ids: undefined}, null, 2));
