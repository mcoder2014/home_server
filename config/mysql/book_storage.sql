create table book_storage
(
    id          bigint auto_increment comment '数据库主键'
        primary key,
    status      int           null comment '状态码',
    type        int           null comment '图书类型：自有、电子书、图书馆外借',
    bid         bigint        not null comment '关联的图书信息 id',
    libraryid   bigint        null comment '关联的图书馆 id',
    isbn13      varchar(13)   null comment '13 位 isbn 编码',
    isbn10      varchar(10)   null comment '10 位 isbn 编码',
    extra       text          null comment '拓展信息',
    create_time datetime      null comment '创建时间',
    update_time datetime      null comment '更新时间',
    quantity    int           null comment '库存数量',
    filename    varchar(1024) null comment '电子图书的文件名',
    dir_path    varchar(4096) null comment '电子图书的文件夹路径'
) comment '图书库存表' ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

