SELECT * FROM products
WHERE
  1 = 0

  OR (
    x LIKE $1
    AND y LIKE $2
    AND z LIKE $3
  )

  OR (
    x LIKE $4
    AND y LIKE $5
    AND z LIKE $6
  )

