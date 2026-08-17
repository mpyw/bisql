SELECT * FROM users
WHERE
  1 = 0

  OR (
    name LIKE ?
    OR email LIKE ?
  )

  OR (
    name LIKE ?
    OR email LIKE ?
  )

