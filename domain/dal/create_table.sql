use home_server;

create table bookinfo
(
    id          bigint auto_increment comment '数据库主键',
    title       varchar(512)  null comment '图书标题',
    author      varchar(256)  null comment '作者',
    publisher   varchar(256)  null comment '出版商',
    pubdate     date          null comment '出版时间',
    isbn13      varchar(13)   null comment '13 位 isbn 编码',
    isbn10      varchar(10)   null comment '10 位 isbn 编码',
    pages       int           null comment '页数',
    price       varchar(128)  null comment '图书价格',
    image       varchar(4096) null comment '封面图片链接',
    summary     text          null comment '摘要',
    create_time datetime      null comment '创建时间',
    update_time datetime      null comment '更新时间',
    constraint isbn_info_id_uindex
        unique (id)
)
    comment 'isbn 图书信息库';

create index isbn_info_isbn10_index
    on bookinfo (isbn10);

create index isbn_info_isbn13_index
    on bookinfo (isbn13);

alter table bookinfo
    add primary key (id);

CREATE TABLE book_storage
(
    id          bigint auto_increment primary key comment '数据库主键',
    status      int           null comment '状态码',
    type        int           null comment '图书类型：自有、电子书、图书馆外借',
    bid         bigint        not null comment '关联的图书信息 id',
    libraryid   bigint        null comment '关联的图书馆 id',
    isbn13      varchar(13)   null comment '13 位 isbn 编码',
    isbn10      varchar(10)   null comment '10 位 isbn 编码',
    extra       text          null comment '拓展信息，比如电子书下载地址等',
    create_time datetime      null comment '创建时间',
    update_time datetime      null comment '更新时间'
)
    comment '图书库存表';

CREATE TABLE book_address
(
    id          bigint auto_increment primary key comment '数据库主键',
    address     varchar(512)  null comment '具体地址信息',
    short_name  varchar(256)  null comment '地址简称',
    create_time datetime      null comment '创建时间',
    update_time datetime      null comment '更新时间'
) comment '图书地址表';

create table login_token
(
    id          bigint                                 null,
    user_id     bigint                                 null,
    token       varchar(256)                           null,
    is_expired  int       default 0                    null,
    create_time timestamp default current_timestamp(6) null,
    update_time timestamp default current_timestamp()  null on update current_timestamp(),
    expire_time timestamp                              null
) comment '登录记录表';


CREATE TABLE webdav_log
(
    id          bigint primary key comment '数据库主键',
    method      varchar(64)   null comment 'HTTP 方法',
    hash        varchar(512)  null comment '文件相对路径哈希值',
    filepath    varchar(4096) null comment '文件相对路径记录',
    user_id     bigint        not null comment '访问者的 user_id',
    agent       varchar(1024) null comment '客户端名称',
    extra       text          null comment '拓展信息，比如电子书下载地址等',
    create_time datetime default current_timestamp(6) comment '创建时间',
    update_time datetime default current_timestamp(6) on update current_timestamp() comment '更新时间'
) comment 'WEBDAV 日志表';