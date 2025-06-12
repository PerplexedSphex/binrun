# Panel Layout Specification

## Overview

Each panel (left, main, right, bottom) can contain a tree of tiles. Tiles are arranged using binary splits or special n-way patterns. No resizing within panels - only fixed proportions.

## Layout Node Types

### 1. Leaf Node (Terminal)
A leaf node displays a single subscription:

```json
{
  "subscription": "event.cpu.host1.freeze"
}
```

### 2. Binary Split Node
Splits space between exactly two children:

```json
{
  "split": "horizontal",  // or "vertical"
  "at": "1/2",           // fraction: "1/3", "2/3", "1/2"
  "first": { ... },      // any node type
  "second": { ... }      // any node type
}
```

**Direction semantics:**
- `horizontal` = split into top/bottom (horizontal line divider)
- `vertical` = split into left/right (vertical line divider)

### 3. Even Split Node
Splits space equally among N children:

```json
{
  "split": "even-3",     // "even-2" through "even-5" supported
  "direction": "horizontal",  // or "vertical"
  "items": [
    { ... },  // any node type
    { ... },
    { ... }
  ]
}
```

## Complete Example

```json
{
  "panels": {
    "left": {
      "split": "horizontal",
      "at": "2/3",
      "first": {
        "subscription": "event.logs.system.freeze"
      },
      "second": {
        "subscription": "event.alerts.critical.freeze"
      }
    },
    "main": {
      "split": "even-3",
      "direction": "vertical",
      "items": [
        {"subscription": "event.cpu.host1.freeze"},
        {"subscription": "event.memory.host1.freeze"},
        {"subscription": "event.disk.host1.freeze"}
      ]
    },
    "right": {
      "subscription": "event.details.freeze"
    },
    "bottom": {
      "subscription": "event.terminal.session.xyz.freeze"
    }
  }
}
```

## Validation Rules

1. **Leaf nodes** must have exactly:
   - `subscription` (string)

2. **Binary split nodes** must have exactly:
   - `split`: "horizontal" | "vertical"
   - `at`: fraction string (e.g. "1/3", "2/3", "1/2")
   - `first`: valid node
   - `second`: valid node

3. **Even split nodes** must have exactly:
   - `split`: "even-2" | "even-3" | "even-4" | "even-5"
   - `direction`: "horizontal" | "vertical"
   - `items`: array of valid nodes (length must match N in "even-N")

4. **Panels** object must have only these keys: "left", "main", "right", "bottom"

5. **No other properties** allowed on any node

## Schema

```typescript
type LayoutNode = LeafNode | BinarySplitNode | EvenSplitNode;

type LeafNode = {
  subscription: string;
};

type BinarySplitNode = {
  split: "horizontal" | "vertical";
  at: "1/2" | "1/3" | "2/3" | "1/4" | "3/4";
  first: LayoutNode;
  second: LayoutNode;
};

type EvenSplitNode = {
  split: "even-2" | "even-3" | "even-4" | "even-5";
  direction: "horizontal" | "vertical";
  items: LayoutNode[];  // length must match N
};

type PanelLayout = {
  panels: {
    left?: LayoutNode;
    main?: LayoutNode;
    right?: LayoutNode;
    bottom?: LayoutNode;
  };
};
```

## Visual Examples

### Binary Split (horizontal, at: "2/3")
```
┌─────────────┐
│             │ 2/3
│   first     │
│             │
├─────────────┤
│   second    │ 1/3
└─────────────┘
```

### Even-3 (direction: vertical)
```
┌────┬────┬────┐
│    │    │    │
│ 0  │ 1  │ 2  │
│    │    │    │
└────┴────┴────┘
```

## Implementation Notes

1. **Fractions only** - no pixels, percentages, or decimals
2. **Common fractions** - stick to 1/2, 1/3, 2/3, 1/4, 3/4 for clarity
3. **Direction naming** - matches CSS flex-direction (horizontal = row of items)
4. **No mixing** - a node is either binary split OR even split, never both
5. **Validation first** - reject invalid layouts before rendering

This spec is intentionally minimal and rigid to ensure LLM-friendly manipulation and foolproof implementation.