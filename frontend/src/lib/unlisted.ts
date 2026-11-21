// unlisted answers a value its closed union leaves out. Go hands the window some values
// without checking them, such as the reason GitHub gives for closing an issue, so a value
// GitHub adds later can still arrive. A switch over such a union names every member as a
// case and returns unlisted(value, answer) from its default: the value is never there to
// the typecheck, so a constant Go adds fails it until the constant has a case, while a value
// from outside the union at run time still gets answer.
export function unlisted<T>(_value: never, answer: T): T {
  return answer
}
