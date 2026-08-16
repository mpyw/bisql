select * /** all columns */ from t
/*%! dynamic conditions */ /*%if conds != null*/where /*%for c in conds*/x = /*c*/0 /*# c_next_and */ /*%end*//*%end*/
