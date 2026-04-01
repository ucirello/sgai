const naturalWorkspaceLabelCollator = new Intl.Collator(undefined, {
  numeric: true,
  sensitivity: "base",
});

export function sortByVisibleLabel<T>(
  items: readonly T[],
  getLabel: (item: T) => string,
  getKey: (item: T) => string,
): T[] {
  return items
    .map((item) => ({ item, label: getLabel(item), key: getKey(item) }))
    .sort((left, right) => {
      const labelComparison = naturalWorkspaceLabelCollator.compare(left.label, right.label);
      if (labelComparison !== 0) {
        return labelComparison;
      }

      return naturalWorkspaceLabelCollator.compare(left.key, right.key);
    })
    .map(({ item }) => item);
}
