# G11 structural measurements (B11-T06)

Same captures (81 of 81 reproduce their recorded digest), same annotations, same matcher. Recognizers: pre-B11, T02, current (rules 3).

| partition | recognizer | fields found | field kind exact | actions found | action effect exact | collections found / annotated | composites recognized / annotated | unannotated composites | workflows supported / annotated | false hints (all / write line crossed) | recognizer p50 ms / max ms / RSS KB |
|---|---|---|---:|---|---:|---|---|---:|---|---|---|
| development + validation (58) | pre | 176/192 | 0.932 | 455/468 | 0.2 | 20/37 | 0/110 | 0 | 18/115 | 137 / 21 | 45.1 / 67.1 / 11248 |
| development + validation (58) | t02 | 186/192 | 0.946 | 455/468 | 0.215 | 35/37 | 0/110 | 0 | 32/115 | 137 / 25 | 44.7 / 79.7 / 11248 |
| development + validation (58) | cur | 189/192 | 0.947 | 455/468 | 0.747 | 36/37 | 93/110 | 98 | 65/115 | 85 / 24 | 44.8 / 73.1 / 11248 |
| held-out AdminLTE + TodoMVC (17, unsealed) | pre | 30/44 | 1 | 66/66 | 0.288 | 2/13 | 0/16 | 0 | 20/47 | 4 / 3 | 47.7 / 61.8 / 11224 |
| held-out AdminLTE + TodoMVC (17, unsealed) | t02 | 30/44 | 1 | 66/66 | 0.288 | 2/13 | 0/16 | 0 | 20/47 | 4 / 3 | 56 / 65 / 11248 |
| held-out AdminLTE + TodoMVC (17, unsealed) | cur | 42/44 | 1 | 66/66 | 0.394 | 11/13 | 11/16 | 15 | 30/47 | 17 / 4 | 46.9 / 63.7 / 11224 |
| held-out File Browser (6, sealed) | pre | 27/28 | 0.926 | 87/89 | 0.253 | 0/4 | 0/16 | 0 | 2/14 | 6 / 4 | 47.6 / 55.7 / 11220 |
| held-out File Browser (6, sealed) | t02 | 27/28 | 0.926 | 87/89 | 0.253 | 0/4 | 0/16 | 0 | 2/14 | 6 / 4 | 41.8 / 46.4 / 11216 |
| held-out File Browser (6, sealed) | cur | 27/28 | 0.926 | 87/89 | 0.379 | 0/4 | 5/16 | 7 | 2/14 | 7 / 5 | 50.8 / 55.7 / 11224 |

False hints are annotation contradictions of non-executed effect hints and field/collection readings; none was promoted to an executable contract. "Write line crossed" counts hints on the other side of the read/write line (write seen as read-only: dangerous direction; read seen as write: a needless prompt).

Write seen as read-only, by partition and recognizer:
- development + validation (58): pre 7, t02 7, cur 7
- held-out AdminLTE + TodoMVC (17, unsealed): pre 1, t02 1, cur 1
- held-out File Browser (6, sealed): pre 0, t02 0, cur 0
