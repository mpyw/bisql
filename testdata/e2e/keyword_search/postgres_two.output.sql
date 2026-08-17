SELECT * FROM users
WHERE
  1 = 0

  OR (
    name LIKE $1
    OR email LIKE $2
  )

  OR (
    name LIKE $3
    OR email LIKE $4
  )

