import {describe, expect, it} from 'vitest'
import {parse} from '@vue/compiler-sfc'
import ts from 'typescript'
import en from './locales/en.json'

const sources = import.meta.glob('./**/*.{ts,vue}', {query: '?raw', import: 'default', eager: true}) as Record<string, string>
const hasChinese = /\p{Script=Han}/u
const catalog = en as Record<string, string>
const translationFunctions = new Set(['t', '$t', 'translate'])

function checkScript(source: string, filename: string, missing: Set<string>) {
  const tree = ts.createSourceFile(filename, source, ts.ScriptTarget.Latest, true)
  function visit(node: ts.Node) {
    if (ts.isCallExpression(node) && translationFunctions.has(node.expression.getText(tree))) {
      const argument = node.arguments[0]
      function checkKey(key: ts.Node | undefined) {
        if (!key) return
        if (ts.isStringLiteralLike(key) && hasChinese.test(key.text) && !(key.text in catalog)) {
          missing.add(`${filename}: ${key.text}`)
        } else if (ts.isConditionalExpression(key)) {
          checkKey(key.whenTrue)
          checkKey(key.whenFalse)
        }
      }
      checkKey(argument)
    }
    ts.forEachChild(node, visit)
  }
  visit(tree)
}

describe('translation source coverage', () => {
  it('includes every literal application translation in the English catalog', () => {
    const missing = new Set<string>()
    for (const [filename, source] of Object.entries(sources)) {
      if (filename.endsWith('.test.ts') || filename.endsWith('.d.ts')) continue
      if (!filename.endsWith('.vue')) {
        checkScript(source, filename, missing)
        continue
      }
      const {descriptor} = parse(source)
      for (const script of [descriptor.script, descriptor.scriptSetup]) {
        if (script) checkScript(script.content, filename, missing)
      }
      const template = descriptor.template?.ast
      function visitTemplate(node: any) {
        if (node.type === 5) checkScript(node.content.content, filename, missing)
        for (const prop of node.props ?? []) {
          if (prop.type === 7 && prop.exp) checkScript(prop.exp.content, filename, missing)
        }
        for (const child of node.children ?? []) visitTemplate(child)
      }
      if (template) visitTemplate(template)
    }
    expect([...missing]).toEqual([])
  })

  it('routes Chinese template text and static labels through translation', () => {
    const untranslated = new Set<string>()
    for (const [filename, source] of Object.entries(sources)) {
      if (!filename.endsWith('.vue')) continue
      const {descriptor} = parse(source)
      function visitTemplate(node: any) {
        // Language names remain in their own language so either locale is easy to find.
        if (node.type === 2 && hasChinese.test(node.content) && node.content.trim() !== '简体中文') {
          untranslated.add(`${filename}: ${node.content.trim()}`)
        }
        for (const prop of node.props ?? []) {
          if (prop.type === 6 && prop.value && hasChinese.test(prop.value.content)) {
            untranslated.add(`${filename}: ${prop.name}=${prop.value.content}`)
          }
        }
        for (const child of node.children ?? []) visitTemplate(child)
      }
      if (descriptor.template?.ast) visitTemplate(descriptor.template.ast)
    }
    expect([...untranslated]).toEqual([])
  })
})
