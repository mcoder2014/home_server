package ebook

import (
	"context"
	"fmt"
	"regexp"
	"strings"

	"github.com/mcoder2014/home_server/utils/log"
	"github.com/mcoder2014/home_server/utils/myfile"
)

type FileScannerCallback func(ctx context.Context, dir, filename string) error

type FileScanner struct {
	// 扫描的文件路径，需要是文件夹
	ScanPaths []string `json:"scan_paths"`
	// 支持的文件后缀名
	FilePostfixes []string `json:"file_postfixes"`
	// 排除的正则表达式
	ExcludeRegexps []string `json:"exclude_regexps"`
	// 回调函数
	Fns []FileScannerCallback
	// 当某个文件 & 回调函数 执行失败时，是否跳过
	SkipError bool
	Errors    []*ErrorRecord
	regexps   []*regexp.Regexp
}

type ErrorRecord struct {
	Dir      string
	FileName string
	Error    error
}

func (s *FileScanner) Scan(ctx context.Context) error {
	var logger = log.Ctx(ctx).WithField("method", "FileScanner.Scan")
	if err := s.initRegexps(); err != nil {
		return fmt.Errorf("init regexp failed, %w", err)
	}
	s.Errors = make([]*ErrorRecord, 0, 100)

	for _idx := 0; _idx < 1e8 && len(s.ScanPaths) > 0; _idx++ {
		curDir := s.ScanPaths[0]
		s.ScanPaths = s.ScanPaths[1:]
		filenames, subDirNames, err := myfile.ListDir(curDir)
		if err != nil {
			if !s.SkipError {
				return fmt.Errorf("scan failed at dir:%v", curDir)
			}
			s.Errors = append(s.Errors, &ErrorRecord{Dir: curDir, FileName: "-", Error: err})
		}
		// 过滤文件和文件夹
		filenames = s.excludeByRegexps(s.filterPostfix(filenames))
		subDirNames = s.excludeByRegexps(subDirNames)
		for _, dir := range subDirNames {
			s.ScanPaths = append(s.ScanPaths, myfile.Join(curDir, dir))
		}
		logger.Infof("curDir:%v files:%v subDirs:%v current scan records:%v", curDir, filenames, subDirNames, s.ScanPaths)
		for _, filename := range filenames {
			for _, f := range s.Fns {
				err := f(ctx, curDir, filename)
				if err != nil {
					if !s.SkipError {
						return fmt.Errorf("scan failed at dir:%v filepath:%v", curDir, filename)
					}
					s.Errors = append(s.Errors, &ErrorRecord{Dir: curDir, FileName: filename, Error: err})
				}
			}
			logger.Infof("cur file path: %v", filename)
		}
	}

	return nil
}

func (s *FileScanner) filterPostfix(filenames []string) []string {
	var res = make([]string, 0, len(filenames))
	for _, name := range filenames {
		name = strings.ToLower(name)
		for _, postfix := range s.FilePostfixes {
			if postfix == "*" {
				res = append(res, name)
				break
			}
			if len(name) > len(postfix) && name[len(name)-len(postfix):] == postfix {
				res = append(res, name)
				break
			}
		}
	}
	return res
}

func (s *FileScanner) initRegexps() error {
	s.regexps = make([]*regexp.Regexp, 0, len(s.ExcludeRegexps))
	for _, reg := range s.ExcludeRegexps {
		res, err := regexp.Compile(reg)
		if err != nil {
			return err
		}
		s.regexps = append(s.regexps, res)
	}
	return nil
}

func (s *FileScanner) excludeByRegexps(names []string) []string {
	if s == nil || len(s.regexps) == 0 {
		return names
	}
	var res = make([]string, 0, len(names))
	for _, name := range names {
		needAdd := true
		for _, reg := range s.regexps {
			if reg.Match([]byte(name)) {
				needAdd = false
				break
			}
		}
		if needAdd {
			res = append(res, name)
		}
	}
	return res
}
