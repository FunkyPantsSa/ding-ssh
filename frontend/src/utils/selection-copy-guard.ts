/** Track selection ownership until the next actual user selection gesture. */
export function guardProgrammaticSelection(terminal: {select(column: number, row: number, length: number): void}) {
  const original = terminal.select
  let programmatic = false
  terminal.select = (column, row, length) => {
    programmatic = true
    original.call(terminal, column, row, length)
  }
  return {
    isProgrammatic: () => programmatic,
    beginUserSelection: () => { programmatic = false },
    dispose: () => { terminal.select = original },
  }
}
