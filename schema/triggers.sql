-- 判题相关触发器。`schema/schema.sql`（mysqldump 结构导出）不含触发器与存储过程，
-- 全新库必须在本文件之后单独执行一次，否则：
--   * `solution.first_time` 永远为 0 —— 排名、首页、用户页的「首杀/AC 统计」恒为空；
--   * `users.coin_earned` 不增长（AC 得金币逻辑在触发器里）。
--
-- 内容取自 HUSTOJ 的 install/db.sql，并与生产库
-- information_schema.TRIGGERS 的 ACTION_STATEMENT 逐字一致。
--
-- 说明：Go 侧只 **读** `first_time=1`（见 schema/queries/{homepage,ranklist}.sql），
-- 不会自己写；所以新库缺 `firstAC` 会直接导致统计错误。

DELIMITER //

-- 回填历史数据的 first_time（全新库为空，等价 no-op；对已有数据的库重放可修正缺失标记）。
-- 先删触发器，避免这条 UPDATE 触发 firstAC 逻辑重复累加金币。
DROP TRIGGER IF EXISTS firstAC//
UPDATE solution s
  JOIN (SELECT user_id, problem_id, MIN(solution_id) AS first_solution_id
          FROM solution WHERE result = 4 GROUP BY user_id, problem_id) t
    ON s.solution_id = t.first_solution_id
  SET s.first_time = 1//

CREATE TRIGGER firstAC BEFORE UPDATE ON solution FOR EACH ROW
BEGIN
  DECLARE acTimes INT;
  DECLARE acCoin INT;
  IF new.result = 4 THEN
    SELECT COUNT(1) INTO acTimes FROM solution
      WHERE problem_id = new.problem_id AND result = 4
        AND first_time = 1 AND user_id = new.user_id;
    SELECT IFNULL(coin, 1) INTO acCoin FROM problem WHERE problem_id = new.problem_id;
    IF acTimes = 0 THEN
      SET new.first_time = 1;
      IF old.first_time = 0 THEN
        UPDATE users SET coin_earned = coin_earned + acCoin WHERE user_id = new.user_id;
      END IF;
    END IF;
  END IF;
END//

-- 相似度比对时把自己和自己比对的结果置空（s_id=0）。
DROP TRIGGER IF EXISTS simfilter//
CREATE TRIGGER simfilter BEFORE INSERT ON sim FOR EACH ROW
BEGIN
  DECLARE new_user_id VARCHAR(64);
  DECLARE old_user_id VARCHAR(64);
  SELECT user_id INTO new_user_id FROM solution WHERE solution_id = new.s_id;
  SELECT user_id INTO old_user_id FROM solution WHERE solution_id = new.sim_s_id;
  IF old_user_id = new_user_id THEN
    SET new.s_id = 0;
  END IF;
END//

DELIMITER ;
